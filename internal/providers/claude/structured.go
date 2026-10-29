package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// defaultStructuredMax bounds the reply when the request sets no cap.
const defaultStructuredMax = 1 << 20

// maxStructuredArgv bounds the fixed system prompt and schema placed in argv.
const maxStructuredArgv = 32 << 10

// Structured implements agents.StructuredRunner.
func (p *Provider) Structured() agents.StructuredSupport {
	return agents.StructuredSupport{
		Supported: true,
		Mechanism: "no tools (claude -p --tools \"\" --restricted --safe-mode)",
	}
}

// structuredArgs builds the argv. Only fixed text and validated option values
// appear here: the user-derived input goes to stdin.
func structuredArgs(req agents.StructuredRequest, settings Settings) []string {
	args := []string{
		"-p", "--output-format", "json", "--json-schema", string(req.Schema),
		"--tools", "", "--restricted", "--strict-mcp-config", "--safe-mode",
		"--no-session-persistence", "--permission-prompts", "none",
	}
	if model := first(req.Model, settings.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := first(req.Effort, settings.Effort); effort != "" {
		args = append(args, "--effort", effort)
	}
	return append(args, "--system-prompt", req.System)
}

// RunStructured implements agents.StructuredRunner. Errors never carry the
// reply or stderr, which may echo the input.
func (p *Provider) RunStructured(ctx context.Context, req agents.StructuredRequest) (json.RawMessage, error) {
	if err := p.ValidateLaunch(req.Account, agents.LaunchOptions{Model: req.Model, Effort: req.Effort}); err != nil {
		return nil, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	if !isJSONObject(req.Schema) || req.System == "" {
		return nil, fmt.Errorf("%w: a structured call needs a schema object and a system prompt", agents.ErrInvalidLaunch)
	}
	// System and Schema go in argv, which other local users can read: a cap keeps
	// a caller from routing user text there by accident.
	if len(req.System) > maxStructuredArgv || len(req.Schema) > maxStructuredArgv {
		return nil, fmt.Errorf("%w: system prompt or schema too large for argv", agents.ErrInvalidLaunch)
	}
	settings, err := ParseSettings(req.Account)
	if err != nil {
		return nil, err
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return nil, err
	}
	dir := configDir(p.accounts, req.Account)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("claude profile %q has no config directory; remove and add it again", req.Account.Name)
	}
	token, err := p.profileToken(settings, req.Account)
	if err != nil {
		return nil, err
	}
	// A fresh empty working directory: nothing of the user's checkout is in reach
	// even if a flag were ignored. It sits under the profile's own data.
	parent := filepath.Join(p.accounts.AccountDir(req.Account.ID), "structured")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("claude structured call: prepare working directory: %w", err)
	}
	work, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		return nil, fmt.Errorf("claude structured call: prepare working directory: %w", err)
	}
	defer os.RemoveAll(work)

	limit := req.MaxOutput
	if limit <= 0 {
		limit = defaultStructuredMax
	}
	out, err := p.runner.Run(ctx, agents.PreparedSession{
		Executable:       executable,
		Args:             structuredArgs(req, settings),
		Dir:              work,
		EnvSet:           profileEnvironment(dir, req.Account, agents.Session{}, token),
		EnvUnsetPrefixes: scrubPrefixes,
	}, RunOptions{Stdin: append([]byte{}, req.Input...), MaxStdout: limit})
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	if errors.Is(err, errOutputTooLarge) {
		return nil, fmt.Errorf("%w: reply exceeded %d bytes", agents.ErrStructuredOutput, limit)
	}
	if err != nil {
		var missing missingBinaryError
		if errors.As(err, &missing) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: claude exited unsuccessfully", agents.ErrStructuredOutput)
	}
	answer, err := parseStructuredReply(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agents.ErrStructuredOutput, err)
	}
	return answer, nil
}

type structuredEnvelope struct {
	Subtype          string          `json:"subtype"`
	IsError          *bool           `json:"is_error"`
	Result           json.RawMessage `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// parseStructuredReply extracts the answer object from `--output-format json`
// stdout. The messages it returns are fixed text: they never quote the reply.
func parseStructuredReply(out []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var env structuredEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, errors.New("reply is not a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("reply has more than one JSON value")
	}
	if env.IsError == nil || *env.IsError || env.Subtype != "success" {
		return nil, errors.New("claude reported an error")
	}
	if isJSONObject(env.StructuredOutput) {
		return compact(env.StructuredOutput)
	}
	var text string
	if json.Unmarshal(env.Result, &text) == nil {
		if text = strings.TrimSpace(text); isJSONObject(json.RawMessage(text)) {
			return compact(json.RawMessage(text))
		}
	}
	return nil, errors.New("reply holds no JSON object answer")
}

func isJSONObject(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && obj != nil
}

func compact(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, errors.New("reply is not valid JSON")
	}
	return buf.Bytes(), nil
}
