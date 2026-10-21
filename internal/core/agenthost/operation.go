package agenthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Receipt phases of an operation-bound start. The receipt is the one record a
// retry or a restarted client trusts to learn what happened to a start.
const (
	phaseReserved     = "reserved"
	phaseClaimed      = "claimed"
	phaseAcknowledged = "acknowledged"
	phaseClosed       = "closed"
)

// tombstoneHash marks a closed record written for an identity whose start never
// reached the disk, so a late start for it is refused as closed.
const tombstoneHash = "tombstone"

// NotStartedError is the fixed reason of a fenced start that never ran.
const NotStartedError = "Implementation holder did not start."

// OperationStartRequest is a start bound to a caller-chosen operation and
// session identity. Repeating the identical request never starts a second
// holder.
type OperationStartRequest struct {
	StartRequest
	OperationID string
	SessionID   agents.SessionID
	Task        agents.TaskRef
}

// OperationObservation is what the receipt and holder say about an operation.
// Acknowledged means the provider itself started, not that the socket was ready.
// Closed means the operation is settled: no holder runs it or can start it later.
type OperationObservation struct {
	Record       Record
	Acknowledged bool
	Closed       bool
}

// validOperationID accepts a short ASCII identity. It is never used to build a
// path: the session ID names the files.
func validOperationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == ':', r == '/', r == '-':
		default:
			return false
		}
	}
	return true
}

func (p Paths) startLock(id string) string { return filepath.Join(p.Records, id+".start.lock") }

// startHash binds everything the holder will run except the prompt text, which
// only contributes its digest.
func startHash(req OperationStartRequest, account agents.Account, dir string, launch agents.LaunchOptions) string {
	digest := sha256.Sum256([]byte(launch.Prompt))
	payload := struct {
		OperationID  string
		SessionID    agents.SessionID
		Task         agents.TaskRef
		Project      string
		Tree         string
		Dir          string
		AccountID    agents.AccountID
		Provider     agents.ProviderID
		Name         string
		Cols, Rows   int
		Model        string
		Options      map[string]string
		PromptDigest string
	}{req.OperationID, req.SessionID, req.Task, req.Project, req.Tree, dir, account.ID, account.Provider, launch.DisplayName, req.Cols, req.Rows, launch.Model, launch.Values(), hex.EncodeToString(digest[:])}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// canonicalDir resolves symlinks when it can, so one directory has one hash.
func canonicalDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return dir
}

func sameTask(a, b *agents.TaskRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func acknowledged(r Record) bool {
	return r.StartPhase == phaseAcknowledged || r.Summary.StartedAt != nil
}

func observe(r Record) OperationObservation {
	return OperationObservation{Record: r, Acknowledged: acknowledged(r), Closed: r.StartPhase == phaseClosed}
}

// StartOperation starts the holder for req once. It records a reserved receipt
// before the launcher runs, so a crash at any point leaves an identity that
// QueryOperation can settle, and a repeated call with the same identity and
// request reconciles the receipt instead of launching again.
func (s Starter) StartOperation(ctx context.Context, req OperationStartRequest) (_ OperationObservation, startErr error) {
	if !validOperationID(req.OperationID) || !validID(string(req.SessionID)) {
		return OperationObservation{}, fmt.Errorf("%w: malformed operation identity", agents.ErrInvalidLaunch)
	}
	prep, err := s.prepare(req.StartRequest)
	if err != nil {
		return OperationObservation{}, err
	}
	account, capabilities := prep.account, prep.capabilities
	launch := prep.launch
	id := string(req.SessionID)
	dir := canonicalDir(req.Dir)
	hash := startHash(req, account, dir, launch)
	if err := s.Paths.ensure(); err != nil {
		return OperationObservation{}, err
	}

	// Fixed order: this session's start lock, then admission. The Garden store
	// transaction lock is never held across either.
	lockCtx, cancel := context.WithTimeout(ctx, admitTimeout)
	defer cancel()
	startLock, err := agents.LockFileContext(lockCtx, s.Paths.startLock(id))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return OperationObservation{}, fmt.Errorf("%w: %w", ErrAdmissionBusy, err)
	case err != nil:
		return OperationObservation{}, fmt.Errorf("cannot lock agent operation: %w", err)
	}
	defer startLock.Unlock()

	existing, err := readReceipt(s.Paths, id)
	switch {
	case err == nil:
		return reconcileReceipt(existing, req, hash)
	case !errors.Is(err, os.ErrNotExist):
		return OperationObservation{}, fmt.Errorf("%w: receipt unreadable", ErrOperationUnknown)
	}

	admission, err := s.admit(ctx)
	if err != nil {
		return OperationObservation{}, err
	}
	defer admission.Unlock()
	records, err := s.admissionRecords()
	if err != nil {
		return OperationObservation{}, err
	}
	if err := checkAdmission(records, account, req.AccountID, capabilities); err != nil {
		return OperationObservation{}, err
	}

	store, ok := s.Sessions.(agents.ReservedSessionStore)
	if !ok {
		return OperationObservation{}, fmt.Errorf("session store cannot reserve session ids")
	}
	runtime, err := store.CreateWithID(account, dir, req.SessionID)
	if err != nil {
		if errors.Is(err, agents.ErrSessionConflict) {
			return OperationObservation{}, fmt.Errorf("%w: %w", ErrOperationConflict, err)
		}
		return OperationObservation{}, fmt.Errorf("cannot create session: %w", err)
	}
	name := req.Name
	if name == "" {
		name = account.Name
	}
	task := req.Task
	summary := Summary{ID: id, ProjectID: req.Project, WorktreeID: req.Tree, AccountID: req.AccountID, Provider: account.Provider, ProfileName: account.Name, Name: name, State: "starting", CreatedAt: time.Now().UTC(), Model: launch.Model, Options: launch.Values(), Origin: s.Origin, OperationID: req.OperationID, Task: &task}
	reserved := Record{Version: RecordVersion, ID: id, Socket: s.Paths.socket(id), Protocol: Version, WorkDir: dir, Summary: summary, StartHash: hash, StartPhase: phaseReserved}
	if err := writeRecord(s.Paths, reserved); err != nil {
		_ = s.Sessions.Cleanup(runtime)
		return OperationObservation{}, fmt.Errorf("cannot record agent operation: %w", err)
	}
	launcher := s.Launch
	if launcher == nil {
		launcher = Launch
	}
	// Any exit but success, a panic included, settles the receipt before the
	// locks go: an unheld lifetime lock is fenced so a delayed holder cannot
	// start after another account is admitted, a held one stays visible as a
	// live holder. The receipt is never removed, and an acknowledged holder is
	// never stopped because this wait ended.
	done := false
	defer func() {
		if done {
			return
		}
		settled, fenced, _ := fenceUnheld(s.Paths, id, "")
		if fenced && !acknowledged(settled) {
			_ = s.Sessions.Cleanup(runtime)
		}
	}()
	if err := ctx.Err(); err != nil {
		return OperationObservation{}, fmt.Errorf("agent start cancelled: %w", err)
	}
	spec := Spec{Summary: summary, WorkDir: dir, Cols: req.Cols, Rows: req.Rows, Launch: launch, StartHash: hash}
	if err := launcher(ctx, s.Paths, spec); err != nil {
		return OperationObservation{}, fmt.Errorf("cannot start agent host: %w", err)
	}
	record, err := readReceipt(s.Paths, id)
	if err != nil {
		return OperationObservation{}, fmt.Errorf("%w: receipt unreadable after launch", ErrOperationUnknown)
	}
	done = true
	return observe(record), nil
}

// reconcileReceipt answers a repeated start from its receipt, without
// launching.
func reconcileReceipt(r Record, req OperationStartRequest, hash string) (OperationObservation, error) {
	if r.Summary.OperationID != req.OperationID {
		return OperationObservation{}, ErrOperationConflict
	}
	obs := observe(r)
	if r.StartPhase == phaseClosed && (r.StartHash == hash || r.StartHash == tombstoneHash) {
		return obs, ErrOperationClosed
	}
	if r.StartHash != hash || !sameTask(r.Summary.Task, &req.Task) {
		return OperationObservation{}, ErrOperationConflict
	}
	return obs, nil
}

// readReceipt reads the record with its liveness but without the ordinary
// lost-holder rewrite, which would hide an unsettled reservation.
func readReceipt(p Paths, id string) (Record, error) {
	r, err := loadRecord(p, id)
	if err != nil {
		return Record{}, err
	}
	r.Alive = held(p.lock(id))
	return r, nil
}

// admissionRecords lists records for an operation's admission. Unlike List it
// fails closed on an unreadable operation receipt: skipping it could admit a
// second account beside a holder that exists.
func (s Starter) admissionRecords() ([]Record, error) {
	entries, err := os.ReadDir(s.Paths.Records)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list agent sessions")
	}
	var out []Record
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !validID(id) {
			continue
		}
		r, err := Read(s.Paths, id)
		if err == nil {
			out = append(out, r)
			continue
		}
		if _, statErr := os.Stat(s.Paths.startLock(id)); statErr == nil {
			return nil, fmt.Errorf("%w: operation receipt %s unreadable", ErrOperationUnknown, id)
		}
	}
	return out, nil
}

// claimReservation is the holder's first act under its lifetime lock: the
// reserved receipt must still be this start's. Only then does it mark the
// receipt claimed, so a fenced or replaced reservation never runs a provider.
func claimReservation(p Paths, spec Spec) error {
	id := spec.Summary.ID
	r, err := loadRecord(p, id)
	if err != nil || r.StartHash != spec.StartHash || r.StartPhase != phaseReserved ||
		r.Summary.OperationID != spec.Summary.OperationID || !sameTask(r.Summary.Task, spec.Summary.Task) {
		return fmt.Errorf("agent operation reservation unavailable")
	}
	r.StartPhase = phaseClaimed
	r.PID = os.Getpid()
	return writeRecord(p, r)
}

// fenceUnheld settles id's receipt when no holder keeps the lifetime lock. It
// takes that lock exclusively, so a holder still forking finds a closed
// receipt when it gets the lock and refuses. fenced is false when a holder
// holds the lock. A terminal status already recorded is kept; a holder that
// had been acknowledged and vanished is lost; one that never was acknowledged
// did not start.
func fenceUnheld(p Paths, id, operationID string) (_ Record, fenced bool, _ error) {
	lock, err := lockFile(p.lock(id))
	if err != nil {
		return Record{}, false, err
	}
	defer lock.Close()
	r, err := loadRecord(p, id)
	now := time.Now().UTC()
	switch {
	case errors.Is(err, os.ErrNotExist):
		if operationID == "" {
			return Record{}, true, err
		}
		r = Record{
			Version: RecordVersion, ID: id, Socket: p.socket(id), Protocol: Version,
			StartHash: tombstoneHash, StartPhase: phaseClosed,
			Summary: Summary{ID: id, Name: "Implementation did not start", State: "failed", Error: NotStartedError, CreatedAt: now, EndedAt: &now, OperationID: operationID},
		}
		return r, true, writeRecord(p, r)
	case err != nil:
		return Record{}, true, err
	}
	if r.StartHash == "" || r.StartPhase == phaseClosed {
		return r, true, nil
	}
	switch {
	case !r.Summary.Active():
	case acknowledged(r):
		r.Summary.State, r.Summary.Error = "failed", LostError
	default:
		r.Summary.State, r.Summary.Error = "failed", NotStartedError
	}
	if !r.Summary.Active() && r.Summary.EndedAt == nil {
		r.Summary.EndedAt = &now
	}
	r.Summary.Activity = nil
	r.StartPhase = phaseClosed
	return r, true, writeRecord(p, r)
}

// QueryOperation reports what happened to an operation's start, fencing it when
// no holder exists. It never starts anything. A reserved receipt nobody holds is
// closed as not started, an acknowledged one whose holder vanished as lost, and
// an identity with no receipt gets a closed tombstone so it can never start
// later. An unreadable receipt or a holder that cannot be read is unknown, never
// absence.
func QueryOperation(ctx context.Context, p Paths, sessionID agents.SessionID, operationID string) (OperationObservation, error) {
	if !agentterminal.Supported {
		return OperationObservation{}, fmt.Errorf("interactive terminals unsupported on this platform")
	}
	id := string(sessionID)
	if p.Records == "" || !validOperationID(operationID) || !validID(id) {
		return OperationObservation{}, fmt.Errorf("%w: malformed operation identity", agents.ErrInvalidLaunch)
	}
	if err := p.ensure(); err != nil {
		return OperationObservation{}, err
	}
	startLock, err := agents.LockFileContext(ctx, p.startLock(id))
	if err != nil {
		return OperationObservation{}, fmt.Errorf("%w: %w", ErrOperationUnknown, err)
	}
	defer startLock.Unlock()

	r, err := readReceipt(p, id)
	switch {
	case errors.Is(err, os.ErrNotExist):
		settled, fenced, ferr := fenceUnheld(p, id, operationID)
		if !fenced || ferr != nil {
			return OperationObservation{}, fmt.Errorf("%w: no receipt for a held session", ErrOperationUnknown)
		}
		return observe(settled), nil
	case err != nil:
		return OperationObservation{}, fmt.Errorf("%w: receipt unreadable", ErrOperationUnknown)
	}
	if r.StartHash == "" || r.Summary.OperationID != operationID {
		return OperationObservation{}, ErrOperationConflict
	}
	if r.StartPhase == phaseClosed {
		return observe(r), nil
	}
	if !r.Alive {
		if settled, fenced, ferr := fenceUnheld(p, id, operationID); fenced {
			if ferr != nil {
				return OperationObservation{}, fmt.Errorf("%w: cannot record fence", ErrOperationUnknown)
			}
			return observe(settled), nil
		}
	}
	return queryHolder(ctx, p, r)
}

// queryHolder reads a live holder. A holder that answers is the evidence. One
// that does not is believed only through a record that already shows it
// acknowledged or ended, because the holder rewrites its record on every
// lifecycle change; before that, a held lock says nothing about the start.
func queryHolder(ctx context.Context, p Paths, r Record) (OperationObservation, error) {
	if r.Socket == "" {
		r.Socket = p.socket(r.ID)
	}
	if info, err := QueryContext(ctx, r); err == nil && info.Summary.ID == r.ID && info.Summary.OperationID == r.Summary.OperationID {
		r.Summary = info.Summary
		r.Alive = true
		obs := observe(r)
		obs.Acknowledged = obs.Acknowledged || info.Summary.StartedAt != nil
		obs.Closed = !info.Summary.Active()
		return obs, nil
	}
	fresh, err := readReceipt(p, r.ID)
	if err == nil && fresh.StartHash == r.StartHash && (acknowledged(fresh) || fresh.StartPhase == phaseClosed) {
		return observe(fresh), nil
	}
	return OperationObservation{}, fmt.Errorf("%w: holder did not answer", ErrOperationUnknown)
}
