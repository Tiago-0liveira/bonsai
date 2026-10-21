package gitbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// GitCaller sends one typed command to the daemon's Git authority. The daemon
// client implements it.
type GitCaller interface {
	GitContext(context.Context, Command) (*Result, error)
}

// envelopeBucket holds the transport metadata of mutation commands: the fixed
// ID, actor and timestamp of the envelope first sent for a step, a hash of its
// arguments and the safe typed result. The daemon hashes the whole envelope, so
// a replay must repeat all of it. It is replay bookkeeping, not the record of
// what the step did.
const envelopeBucket = "isolation_envelopes"

const (
	envelopeSent      = "sent"
	envelopeDone      = "done"
	envelopeFailed    = "failed"
	envelopeUncertain = "uncertain"
)

type envelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	UserID    string          `json:"user_id"`
	CreatedAt time.Time       `json:"created_at"`
	ArgsHash  string          `json:"args_hash"`
	State     string          `json:"state"`
	Code      string          `json:"code,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// IsolationClient adapts the daemon's typed isolation commands to
// domain.IsolationService. It decodes strictly, checks that every result names
// the command it answers, and preserves the daemon's domain errors.
type IsolationClient struct {
	Caller       GitCaller
	RepositoryID string
	UserID       string
	Now          func() time.Time
	NewID        func() string
	Journal      *gitstore.Store
}

var _ domain.IsolationService = (*IsolationClient)(nil)

func (c *IsolationClient) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *IsolationClient) command(id, kind string, raw json.RawMessage, userID string, at time.Time) Command {
	return Command{ID: id, UserID: userID, RepositoryID: c.RepositoryID, Type: kind, Arguments: raw, CreatedAt: at}
}

func internalError(message string) error { return domain.E("internal", message) }

// decode reads a result: its error first, then a payload of exactly the
// expected type.
func decode(res *Result, id string, out any) error {
	if res == nil || res.ID != id {
		return internalError("daemon answered a different command")
	}
	if res.Error != nil {
		return res.Error
	}
	return decodePayload(res.Payload, out)
}

func decodePayload(payload json.RawMessage, out any) error {
	if out == nil || len(bytes.TrimSpace(payload)) == 0 || string(bytes.TrimSpace(payload)) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return internalError("daemon returned an unexpected result")
	}
	if dec.Decode(new(any)) != io.EOF {
		return internalError("daemon returned an unexpected result")
	}
	return nil
}

func (c *IsolationClient) read(ctx context.Context, kind, key string, value, out any) error {
	if c.Caller == nil || c.NewID == nil {
		return internalError("isolation client is not configured")
	}
	id := c.NewID()
	res, err := c.Caller.GitContext(ctx, c.command(id, kind, MarshalArguments(map[string]any{key: value}), c.UserID, c.now()))
	if err != nil {
		return err
	}
	return decode(res, id, out)
}

func (c *IsolationClient) PreviewIsolation(ctx context.Context, req domain.IsolationRequest) (domain.IsolationProposal, error) {
	var out domain.IsolationProposal
	err := c.read(ctx, "git.isolation.preview", "isolation_request", req, &out)
	return out, err
}

func (c *IsolationClient) InspectIsolation(ctx context.Context, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	var out domain.IsolationCheck
	err := c.read(ctx, "git.isolation.inspect", "isolation_proposal", p, &out)
	return out, err
}

func (c *IsolationClient) InspectPublishedHead(ctx context.Context, p domain.IsolationProposal) (domain.PublishedHead, error) {
	var out domain.PublishedHead
	err := c.read(ctx, "git.isolation.head", "isolation_proposal", p, &out)
	return out, err
}

// CreateIsolation sends the creation under the stable ID `<operation>/worktree`.
// After an outcome that is unknown, a second send needs proof first: the worktree
// is inspected, and only an owned step that still needs work is sent again, under
// a recovery ID. Nothing is ever added blindly.
func (c *IsolationClient) CreateIsolation(ctx context.Context, req domain.IsolationCreateRequest) (domain.IsolationCheck, error) {
	var out domain.IsolationCheck
	resolve := func(ctx context.Context) (bool, json.RawMessage, error) {
		check, err := c.InspectIsolation(ctx, req.Proposal)
		if err != nil {
			return false, nil, err
		}
		if check.Exists && check.Owned && check.Hooks == "done" {
			raw, _ := json.Marshal(check)
			return true, raw, nil
		}
		return false, nil, nil
	}
	err := c.mutate(ctx, req.Proposal.OperationID+"/worktree", "git.isolation.create", "isolation_create", req, resolve, &out)
	return out, err
}

// RemoveIsolation sends the removal under `<operation>/cleanup/<fingerprint>`, so
// it can only repeat the confirmed state.
func (c *IsolationClient) RemoveIsolation(ctx context.Context, req domain.IsolationRemoveRequest) error {
	p := req.Check.Proposal
	resolve := func(ctx context.Context) (bool, json.RawMessage, error) {
		check, err := c.InspectIsolation(ctx, p)
		if err != nil {
			return false, nil, err
		}
		switch {
		case !check.Exists:
			return true, json.RawMessage("null"), nil
		case check.Owned:
			return false, nil, nil
		default:
			return false, nil, domain.ErrOwnership
		}
	}
	return c.mutate(ctx, p.OperationID+"/cleanup/"+req.Check.Fingerprint, "git.isolation.remove", "isolation_remove", req, resolve, nil)
}

// PushIsolation sends the push under `<operation>/push/<head>`.
func (c *IsolationClient) PushIsolation(ctx context.Context, req domain.IsolationPushRequest) (domain.PublishedHead, error) {
	var out domain.PublishedHead
	resolve := func(ctx context.Context) (bool, json.RawMessage, error) {
		head, err := c.InspectPublishedHead(ctx, req.Proposal)
		if err != nil {
			return false, nil, err
		}
		if head.RemoteVerified && head.RemoteSHA == req.HeadSHA && head.HeadSHA == req.HeadSHA {
			raw, _ := json.Marshal(head)
			return true, raw, nil
		}
		return false, nil, nil
	}
	err := c.mutate(ctx, req.Proposal.OperationID+"/push/"+req.HeadSHA, "git.isolation.push", "isolation_push", req, resolve, &out)
	return out, err
}

func recoveryID(base string, n int) string {
	if n == 0 {
		return base
	}
	return base + "/recover/" + strconv.Itoa(n)
}

func (c *IsolationClient) loadFamily(base string) ([]envelope, error) {
	var out []envelope
	err := c.Journal.View(func(d gitstore.Data) error {
		for n := 0; ; n++ {
			env, ok := gitstore.Get[envelope](d, envelopeBucket, recoveryID(base, n))
			if !ok {
				return nil
			}
			out = append(out, env)
		}
	})
	return out, err
}

func (c *IsolationClient) saveEnvelope(env envelope) error {
	return c.Journal.Update(func(d gitstore.Data) error { return gitstore.Put(d, envelopeBucket, env.ID, env) })
}

func (c *IsolationClient) settle(id, state, code string, result json.RawMessage) error {
	return c.Journal.Update(func(d gitstore.Data) error {
		env, ok := gitstore.Get[envelope](d, envelopeBucket, id)
		if !ok {
			return errors.New("envelope missing")
		}
		env.State, env.Code, env.Result = state, code, result
		return gitstore.Put(d, envelopeBucket, id, env)
	})
}

// mutate sends one mutation. The first envelope for a step keeps its ID, actor
// and timestamp across restarts; a replay repeats it byte for byte so the daemon
// recognizes it. A step whose last outcome is unknown must be resolved by
// inspection before another envelope is sent; one that failed definitely gets a
// new recovery envelope, because the daemon replays the failure for the old ID.
func (c *IsolationClient) mutate(ctx context.Context, base, kind, key string, value any, resolve func(context.Context) (bool, json.RawMessage, error), out any) error {
	if c.Caller == nil || c.Journal == nil {
		return internalError("isolation envelope journal unavailable")
	}
	raw := MarshalArguments(map[string]any{key: value})
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	family, err := c.loadFamily(base)
	if err != nil {
		return domain.ErrUncertain
	}
	var env envelope
	if n := len(family); n > 0 {
		last := family[n-1]
		switch {
		case last.State == envelopeDone && last.ArgsHash == hash:
			env = last
		case last.State == envelopeSent || last.State == envelopeUncertain:
			if resolve != nil {
				done, payload, err := resolve(ctx)
				if err != nil {
					var de *domain.Error
					if errors.As(err, &de) {
						return err
					}
					return fmt.Errorf("%w: inspection failed", domain.ErrUncertain)
				}
				if done {
					if err := c.settle(last.ID, envelopeDone, "", payload); err != nil {
						return domain.ErrUncertain
					}
					return decodePayload(payload, out)
				}
			}
		}
		if env.ID == "" {
			env = envelope{ID: recoveryID(base, n), Type: kind, UserID: c.UserID, CreatedAt: c.now(), ArgsHash: hash, State: envelopeSent}
		}
	} else {
		env = envelope{ID: base, Type: kind, UserID: c.UserID, CreatedAt: c.now(), ArgsHash: hash, State: envelopeSent}
	}
	if env.State == envelopeSent && (len(family) == 0 || family[len(family)-1].ID != env.ID) {
		if err := c.saveEnvelope(env); err != nil {
			return domain.ErrUncertain
		}
	}
	res, err := c.Caller.GitContext(ctx, c.command(env.ID, kind, raw, env.UserID, env.CreatedAt))
	if err != nil {
		_ = c.settle(env.ID, envelopeUncertain, "", nil)
		return fmt.Errorf("%w: command outcome unknown", domain.ErrUncertain)
	}
	if res == nil || res.ID != env.ID {
		_ = c.settle(env.ID, envelopeUncertain, "", nil)
		return internalError("daemon answered a different command")
	}
	if res.Error != nil {
		state := envelopeFailed
		if res.Error.Code == domain.Code(domain.ErrUncertain) || res.Error.Code == "expired" {
			state = envelopeUncertain
		}
		_ = c.settle(env.ID, state, res.Error.Code, nil)
		// A failure after the resource existed still reports what exists.
		if len(res.Payload) > 0 {
			_ = decodePayload(res.Payload, out)
		}
		return res.Error
	}
	if err := decodePayload(res.Payload, out); err != nil {
		_ = c.settle(env.ID, envelopeUncertain, "", nil)
		return err
	}
	if err := c.settle(env.ID, envelopeDone, "", res.Payload); err != nil {
		return domain.ErrUncertain
	}
	return nil
}
