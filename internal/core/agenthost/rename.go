package agenthost

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxNameRunes bounds a session name so a rename cannot grow every list row,
// notification and record without limit.
const MaxNameRunes = 80

// ErrNameInvalid means a rename asked for an empty name, or one too long.
var ErrNameInvalid = errors.New("invalid agent session name")

// ErrRenameUnsupported means the holder predates renaming. It keeps running
// under its old name; a restart picks up the new protocol.
var ErrRenameUnsupported = errors.New("agent host cannot rename sessions")

// ErrRenameBusy means the record changed hands while a rename was settling.
var ErrRenameBusy = errors.New("agent session is changing; try again")

// CleanName trims name, drops control characters (a name is a one-line label
// and ends up in terminal titles) and checks the length.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if name == "" || utf8.RuneCountInString(name) > MaxNameRunes {
		return "", ErrNameInvalid
	}
	return name, nil
}

// Rename sets a session's display name and returns the updated summary. A
// running holder owns its record, so it applies the change and republishes;
// an ended session has no holder, so its record is rewritten under the
// lifetime lock a live holder would keep.
func Rename(p Paths, id, name string) (Summary, error) {
	name, err := CleanName(name)
	if err != nil {
		return Summary{}, err
	}
	r, err := Read(p, id)
	if err != nil {
		return Summary{}, err
	}
	if r.Alive {
		return renameHolder(r, name)
	}
	if r.StartPhase == phaseReserved || r.StartPhase == phaseClaimed {
		return Summary{}, fmt.Errorf("agent session %s has an unsettled start; query it first", id)
	}
	return renameEndedLocked(p, id, name)
}

// renameEndedLocked rewrites the name in a record no holder keeps.
func renameEndedLocked(p Paths, id, name string) (Summary, error) {
	lock, err := lockFile(p.lock(id))
	if err != nil {
		// A holder took the lock after the liveness read.
		return Summary{}, ErrRenameBusy
	}
	defer lock.Close()
	raw, err := loadRecord(p, id)
	if err != nil {
		return Summary{}, err
	}
	raw.Summary.Name = name
	if err := writeRecord(p, raw); err != nil {
		return Summary{}, err
	}
	return raw.Summary, nil
}

func renameHolder(r Record, name string) (Summary, error) {
	c, _, err := Dial(r, Request{Mode: ModeStatus, Client: "tui"})
	if err != nil {
		return Summary{}, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if err := c.Send(Request{Type: "rename", Name: name}); err != nil {
		return Summary{}, err
	}
	for {
		f, err := c.Recv()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return Summary{}, err
			}
			return Summary{}, fmt.Errorf("rename agent session: %w", err)
		}
		switch f.Type {
		case "renamed":
			if f.Status == nil {
				return Summary{}, errors.New("rename agent session: empty reply")
			}
			return *f.Status, nil
		case "error":
			if f.Error == "unknown request" {
				return Summary{}, ErrRenameUnsupported
			}
			return Summary{}, errors.New(f.Error)
		}
	}
}
