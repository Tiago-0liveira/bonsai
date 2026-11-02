package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// maxMetaLine caps the first rollout line we read: it holds the session_meta
// record, and a rollout with a larger one is not something to trust.
const maxMetaLine = 64 << 10

// rolloutSkew tolerates the gap between Bonsai recording the start and codex
// stamping the rollout.
const rolloutSkew = 5 * time.Second

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type rolloutMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Cwd       string `json:"cwd"`
	} `json:"payload"`
}

// DiscoverSessionID finds the rollout codex wrote for this session. It returns
// "" until exactly one unclaimed rollout matches; an ambiguous result keeps the
// holder polling because another record may claim its own id meanwhile.
func (p *Provider) DiscoverSessionID(_ context.Context, req agents.ResumeRequest) (string, error) {
	id, _, err := findRollout(homeDir(p.accounts, req.Account), req.Session.WorkDir, req.StartedAt, req.Claimed)
	return id, err
}

// ResumeTarget checks the conversation is on disk. A recorded id must have a
// rollout file; without one the rollout is rediscovered by cwd and start time.
func (p *Provider) ResumeTarget(_ context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
	home := homeDir(p.accounts, req.Account)
	root := filepath.Join(home, "sessions")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNoConversation)
	}
	if req.ProviderSessionID != "" {
		if uuidPattern.MatchString(req.ProviderSessionID) && rolloutExists(root, req.ProviderSessionID) {
			return agents.ResumeTarget{ID: req.ProviderSessionID}, nil
		}
		return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNotFound)
	}
	id, _, err := findRollout(home, req.Session.WorkDir, req.StartedAt, req.Claimed)
	if err != nil {
		return agents.ResumeTarget{}, err
	}
	if id == "" {
		return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNotFound)
	}
	return agents.ResumeTarget{ID: id}, nil
}

// rolloutExists walks sessions/YYYY/MM/DD (depth 4 with the file) for a rollout
// whose name ends in -<id>.jsonl.
func rolloutExists(root, id string) bool {
	suffix := "-" + id + ".jsonl"
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= 3 {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), suffix) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// findRollout scans the date directories around startedAt (codex names them in
// local time) for rollouts started in workDir after startedAt and not claimed
// by another record. Exactly one match returns its id; several return
// ambiguous with no id.
func findRollout(home, workDir string, startedAt time.Time, claimed []string) (id string, ambiguous bool, err error) {
	var dirs []string
	for _, day := range []time.Time{startedAt.AddDate(0, 0, -1), startedAt, time.Now()} {
		local := day.Local()
		dir := filepath.Join(home, "sessions", local.Format("2006"), local.Format("01"), local.Format("02"))
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	want := filepath.Clean(workDir)
	notBefore := startedAt.Add(-rolloutSkew)
	var matches []string
	for _, dir := range dirs {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			if errors.Is(readErr, fs.ErrNotExist) {
				continue
			}
			return "", false, readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			meta, ok := readRolloutMeta(filepath.Join(dir, name))
			if !ok || !uuidPattern.MatchString(meta.Payload.ID) || filepath.Clean(meta.Payload.Cwd) != want || slices.Contains(claimed, meta.Payload.ID) {
				continue
			}
			ts, parseErr := time.Parse(time.RFC3339Nano, meta.Payload.Timestamp)
			if parseErr != nil || ts.Before(notBefore) {
				continue
			}
			if !slices.Contains(matches, meta.Payload.ID) {
				matches = append(matches, meta.Payload.ID)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", false, nil
	case 1:
		return matches[0], false, nil
	}
	return "", true, nil
}

// readRolloutMeta decodes the first line when it is a session_meta record that
// fits in maxMetaLine.
func readRolloutMeta(path string) (rolloutMeta, bool) {
	file, err := os.Open(path)
	if err != nil {
		return rolloutMeta{}, false
	}
	defer file.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(file, maxMetaLine+1), 4096).ReadBytes('\n')
	if (err != nil && !errors.Is(err, io.EOF)) || len(line) > maxMetaLine {
		return rolloutMeta{}, false
	}
	var meta rolloutMeta
	if json.Unmarshal(line, &meta) != nil || meta.Type != "session_meta" {
		return rolloutMeta{}, false
	}
	return meta, true
}

var (
	_ agents.Resumer             = (*Provider)(nil)
	_ agents.SessionIDDiscoverer = (*Provider)(nil)
)
