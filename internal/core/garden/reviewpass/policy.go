package reviewpass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type Native struct {
	PermissionMode string `json:"permission_mode,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
	Approval       string `json:"approval,omitempty"`
	FullAccess     *bool  `json:"full_access,omitempty"`
}

func (n Native) launch(model, effort string) agents.LaunchOptions {
	l := agents.LaunchOptions{Model: model, Effort: effort, PermissionMode: n.PermissionMode, Sandbox: n.Sandbox, Approval: n.Approval}
	if n.FullAccess != nil {
		v := *n.FullAccess
		l.FullAccess = &v
	}
	return l
}
func nativeFrom(l agents.LaunchOptions) Native {
	n := Native{PermissionMode: l.PermissionMode, Sandbox: l.Sandbox, Approval: l.Approval}
	if l.FullAccess != nil {
		v := *l.FullAccess
		n.FullAccess = &v
	}
	return n
}

type Reviewer struct {
	Provider    agents.ProviderID `json:"provider"`
	AccountID   agents.AccountID  `json:"account_id"`
	AccountName string            `json:"account_name"`
	Model       string            `json:"model,omitempty"`
	Effort      string            `json:"effort,omitempty"`
	Native      Native            `json:"native"`
	Escalated   bool              `json:"escalated"`
}

const Passes = 1

type Policy struct {
	Reviewer Reviewer `json:"reviewer"`
	Passes   int      `json:"passes"`
	Checks   []Check  `json:"checks,omitempty"`
}

func (p Policy) Validate() error {
	r := p.Reviewer
	l := r.Native.launch(r.Model, r.Effort)
	if p.Passes != Passes || r.Provider == "" || r.AccountID == "" {
		return fmt.Errorf("%w: policy identity", ErrInvalid)
	}
	if err := agents.NativePermissions(r.Provider, l); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if r.Escalated != agents.EscalatedLaunch(r.Provider, l) {
		return fmt.Errorf("%w: escalation", ErrInvalid)
	}
	prefs := config.GardenAutoReviewPrefs{Account: string(r.AccountID), Model: r.Model, Effort: r.Effort, PermissionMode: l.PermissionMode, Sandbox: l.Sandbox, Approval: l.Approval, FullAccess: l.FullAccess}
	seen := map[string]bool{}
	for _, c := range p.Checks {
		if c.ID != CheckFrom(c.config()).ID || seen[c.ID] {
			return fmt.Errorf("%w: check identity", ErrInvalid)
		}
		seen[c.ID] = true
		prefs.Checks = append(prefs.Checks, c.config())
	}
	if err := prefs.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

// Hash uses struct field order and policy check order, never map iteration.
func (p Policy) Hash() string { b, _ := json.Marshal(p); return fmt.Sprintf("%x", sha256.Sum256(b)) }
