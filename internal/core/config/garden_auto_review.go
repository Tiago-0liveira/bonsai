package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

const (
	GardenAutoReviewVersion = 1
	MaxGardenChecks         = 8
	MaxGardenCheckArgs      = 32
	MaxGardenCheckArgBytes  = 256
	MaxGardenCheckNameRunes = 60
)

// GardenAutoReviewPrefs is local bounded-review configuration. Nil means off.
// Its existence grants no permission to start anything: each run needs an explicit
// local confirmation. Nothing remote may build this policy.
type GardenAutoReviewPrefs struct {
	Version        int           `json:"version"`
	Enabled        bool          `json:"enabled"`
	Account        string        `json:"account,omitempty"`
	Model          string        `json:"model,omitempty"`
	Effort         string        `json:"effort,omitempty"`
	PermissionMode string        `json:"permission_mode,omitempty"`
	Sandbox        string        `json:"sandbox,omitempty"`
	Approval       string        `json:"approval,omitempty"`
	FullAccess     *bool         `json:"full_access,omitempty"`
	Checks         []GardenCheck `json:"checks,omitempty"`
}

type GardenCheck struct {
	Name       string   `json:"name"`
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
	// Dir is relative to the worktree root; empty means the root.
	Dir string `json:"dir,omitempty"`
}

var (
	gardenExecutableRE = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	gardenShellRE      = regexp.MustCompile(`^r?(sh|bash|zsh|fish|dash|ksh|csh|tcsh|ash|mksh|yash|rc|es|elvish|nu|xonsh|ion|osh|ysh|oil|scsh|tclsh|wish|cmd|powershell|pwsh)[0-9.-]*$`)
)

func (p GardenAutoReviewPrefs) Validate() error {
	invalid := func(field string) error { return fmt.Errorf("%w: %s", ErrInvalidGardenAutoReview, field) }
	if p.Version < 0 || p.Version > GardenAutoReviewVersion {
		return invalid("version")
	}
	if p.Account != strings.TrimSpace(p.Account) || len(p.Account) > 128 || !utf8.ValidString(p.Account) || strings.IndexFunc(p.Account, unicode.IsControl) >= 0 {
		return invalid("account")
	}
	if p.Enabled && p.Account == "" {
		return invalid("account required")
	}
	for _, s := range []string{p.Model, p.Effort, p.PermissionMode, p.Sandbox, p.Approval} {
		if len(s) > 64 || !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
			return invalid("launch value")
		}
	}
	if len(p.Checks) > MaxGardenChecks {
		return invalid("checks")
	}
	seen := map[string]bool{}
	for _, c := range p.Checks {
		if !utf8.ValidString(c.Name) || utf8.RuneCountInString(c.Name) < 1 || utf8.RuneCountInString(c.Name) > MaxGardenCheckNameRunes || strings.IndexFunc(c.Name, func(r rune) bool { return !unicode.IsPrint(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
			return invalid("check name")
		}
		if !utf8.ValidString(c.Executable) || strings.IndexFunc(c.Executable, unicode.IsControl) >= 0 || len(c.Executable) > 256 || (!gardenExecutableRE.MatchString(c.Executable) && !(filepath.IsAbs(c.Executable) && filepath.Clean(c.Executable) == c.Executable)) {
			return invalid("check executable")
		}
		base := strings.TrimSuffix(strings.ToLower(filepath.Base(c.Executable)), ".exe")
		switch strings.ToLower(filepath.Ext(c.Executable)) {
		case ".cmd", ".bat", ".ps1":
			return invalid("shell executable")
		}
		if gardenShellRE.MatchString(base) {
			return invalid("shell executable")
		}
		switch base {
		case "env", "sudo", "doas", "su", "xargs", "nohup", "busybox":
			return invalid("shell executable")
		}
		if len(c.Args) > MaxGardenCheckArgs {
			return invalid("check args")
		}
		for _, arg := range c.Args {
			if len(arg) > MaxGardenCheckArgBytes || !utf8.ValidString(arg) || strings.IndexFunc(arg, unicode.IsControl) >= 0 || strings.ContainsAny(arg, "|&;<>$`") {
				return invalid("check arg")
			}
		}
		if len(c.Dir) > 256 || !utf8.ValidString(c.Dir) || strings.IndexFunc(c.Dir, unicode.IsControl) >= 0 || (c.Dir != "" && (path.IsAbs(c.Dir) || c.Dir == "." || path.Clean(c.Dir) != c.Dir || strings.ContainsAny(c.Dir, ":\\") || hasParent(c.Dir))) {
			return invalid("check dir")
		}
		// Nil and empty argv describe the same executable invocation.
		args := c.Args
		if args == nil {
			args = []string{}
		}
		key, _ := json.Marshal(struct {
			Executable string
			Args       []string
			Dir        string
		}{c.Executable, args, c.Dir})
		if seen[string(key)] {
			return invalid("duplicate check")
		}
		seen[string(key)] = true
	}
	return nil
}

func hasParent(s string) bool {
	for _, part := range strings.Split(s, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func (p *GardenAutoReviewPrefs) Clone() *GardenAutoReviewPrefs {
	if p == nil {
		return nil
	}
	out := *p
	if p.FullAccess != nil {
		v := *p.FullAccess
		out.FullAccess = &v
	}
	if p.Checks != nil {
		out.Checks = make([]GardenCheck, len(p.Checks))
		for i, c := range p.Checks {
			out.Checks[i] = c
			if c.Args != nil {
				out.Checks[i].Args = append([]string{}, c.Args...)
			}
		}
	}
	return &out
}

// ParseGardenChecks accepts semicolon-separated checks without quoting. Words
// cannot contain spaces; executable and arguments are separated by whitespace.
func ParseGardenChecks(text string) ([]GardenCheck, error) {
	var checks []GardenCheck
	for _, part := range strings.Split(text, ";") {
		words := strings.Fields(strings.TrimSpace(part))
		if len(words) == 0 {
			continue
		}
		name := []rune(strings.Join(words, " "))
		if len(name) > MaxGardenCheckNameRunes {
			name = append(name[:MaxGardenCheckNameRunes-1], '…')
		}
		check := GardenCheck{Name: string(name), Executable: words[0]}
		// Nil, not empty, so a check without arguments equals its saved and reloaded form.
		if len(words) > 1 {
			check.Args = words[1:]
		}
		checks = append(checks, check)
	}
	if err := (GardenAutoReviewPrefs{Checks: checks}).Validate(); err != nil {
		return nil, err
	}
	return checks, nil
}

// FormatGardenChecks formats the unquoted Settings form. Words cannot contain
// spaces. Dir and custom display names are retained only by the typed form.
func FormatGardenChecks(checks []GardenCheck) string {
	parts := make([]string, len(checks))
	for i, c := range checks {
		parts[i] = strings.Join(append([]string{c.Executable}, c.Args...), " ")
	}
	return strings.Join(parts, "; ")
}

// canonical rejects parser aliases and discarded fields, including nested ones.
func canonical(raw []byte, v any) bool {
	if !utf8.Valid(raw) || garden.RejectDuplicateJSONKeys(raw) != nil || json.Unmarshal(raw, v) != nil {
		return false
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var before, after any
	original := json.NewDecoder(bytes.NewReader(raw))
	original.UseNumber()
	roundTrip := json.NewDecoder(bytes.NewReader(encoded))
	roundTrip.UseNumber()
	return original.Decode(&before) == nil && roundTrip.Decode(&after) == nil && reflect.DeepEqual(before, after)
}
