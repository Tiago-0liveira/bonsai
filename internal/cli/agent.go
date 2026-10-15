package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type agentRuntime struct {
	accounts       agents.AccountStore
	accountService *agents.AccountService
	sessionService *agents.SessionService
	usageService   *agents.UsageService
}

func newAgentRuntime(in io.Reader, out, errOut io.Writer) (*agentRuntime, error) {
	r, err := agentruntime.New(in, out, errOut)
	if err != nil {
		return nil, err
	}
	return &agentRuntime{accounts: r.Accounts, accountService: r.AccountService, sessionService: r.SessionService, usageService: r.UsageService}, nil
}

func cmdAgent(args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		printAgentUsage(errOut)
		return fmt.Errorf("usage: bonsai agent <account|run|usage> ...")
	}
	runtime, err := newAgentRuntime(in, out, errOut)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch args[0] {
	case "account":
		return cmdAgentAccount(ctx, runtime, args[1:], in, out, errOut)
	case "run":
		return cmdAgentRun(ctx, runtime, args[1:])
	case "usage":
		return cmdAgentUsage(ctx, runtime, args[1:], out, errOut)
	case "help", "-h", "--help":
		printAgentUsage(out)
		return nil
	default:
		printAgentUsage(errOut)
		return fmt.Errorf("unknown agent command %q", args[0])
	}
}

func cmdAgentAccount(ctx context.Context, runtime *agentRuntime, args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bonsai agent account <add|login|list|show|set|rename|remove>")
	}
	switch args[0] {
	case "add":
		provider, name, options, err := parseAccountAddArgs(args[1:], in)
		if err != nil {
			return err
		}
		result, err := runtime.accountService.SetupWithIO(ctx, provider, name, options, agents.SetupIO{})
		printSetupWarnings(out, result.Warnings)
		if err != nil {
			return err
		}
		account := result.Account
		fmt.Fprintf(out, "added %s account %q (%s)\n", account.Provider, account.Name, account.ID)
		return nil
	case "login":
		selector, options, err := parseAccountLoginArgs(args[1:], in)
		if err != nil {
			return err
		}
		account, err := agents.ResolveAccount(runtime.accounts, selector)
		if err != nil {
			return err
		}
		result, err := runtime.accountService.Reauthenticate(ctx, account.ID, options, agents.SetupIO{})
		printSetupWarnings(out, result.Warnings)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "signed in %s account %q\n", result.Account.Provider, result.Account.Name)
		return nil
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: bonsai agent account list")
		}
		accounts, err := runtime.accountService.List(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tPROVIDER\tAUTH\tIDENTITY\tPLAN")
		for _, account := range accounts {
			info := runtime.accountService.Registry.DescribeAccount(ctx, account)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", account.Name, account.Provider, info.AuthMode, info.Identity, info.Plan)
		}
		return tw.Flush()
	case "rename":
		if len(args) != 3 {
			return fmt.Errorf("usage: bonsai agent account rename <account> <new-name>")
		}
		account, err := agents.ResolveAccount(runtime.accounts, args[1])
		if err != nil {
			return err
		}
		renamed, err := runtime.accountService.Rename(ctx, account.ID, args[2])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "renamed %s account to %q\n", renamed.Provider, renamed.Name)
		return nil
	case "remove", "rm", "delete":
		if len(args) != 2 {
			return fmt.Errorf("usage: bonsai agent account remove <account>")
		}
		account, err := agents.ResolveAccount(runtime.accounts, args[1])
		if err != nil {
			return err
		}
		// Sessions started by `agent run` or the web keep a record until they
		// end; removing the profile takes their provider home away.
		if records, err := runtime.accountService.SessionRecords(account.ID); err == nil && len(records) > 0 {
			fmt.Fprintf(errOut, "warning: %s still has %d session record(s); a running agent loses its profile\n", account.Name, len(records))
		}
		warnings, err := runtime.accountService.Remove(ctx, account.ID, agents.RemoveOptions{})
		for _, warning := range warnings {
			fmt.Fprintf(errOut, "warning: %s\n", warning)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %s account %q\n", account.Provider, account.Name)
		return nil
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: bonsai agent account show <account>")
		}
		account, err := agents.ResolveAccount(runtime.accounts, args[1])
		if err != nil {
			return err
		}
		return printAccount(ctx, out, runtime.accountService.Registry, account)
	case "set":
		account, defaults, err := parseAccountSetArgs(runtime.accounts, args[1:])
		if err != nil {
			return err
		}
		updated, err := runtime.accountService.UpdateDefaults(ctx, account.ID, defaults)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "updated %s account %q defaults\n", updated.Provider, updated.Name)
		return nil
	default:
		return fmt.Errorf("unknown agent account command %q", args[0])
	}
}

const accountSetUsage = "usage: bonsai agent account set <account> <field>=<value>... [--unset <field>]..."

// parseAccountSetArgs parses `account set`. Field names and values are checked
// by the provider, not here.
func parseAccountSetArgs(store agents.AccountStore, args []string) (agents.Account, map[string]*string, error) {
	var selector string
	defaults := map[string]*string{}
	add := func(field string, value *string) error {
		if _, dup := defaults[field]; dup {
			return fmt.Errorf("%s is given more than once", field)
		}
		defaults[field] = value
		return nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--unset" || strings.HasPrefix(arg, "--unset="):
			field, hasValue := strings.CutPrefix(arg, "--unset=")
			if !hasValue {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return agents.Account{}, nil, fmt.Errorf("--unset requires a field")
				}
				i++
				field = args[i]
			}
			if field == "" || strings.Contains(field, "=") {
				return agents.Account{}, nil, fmt.Errorf("--unset requires a field")
			}
			if err := add(field, nil); err != nil {
				return agents.Account{}, nil, err
			}
		case strings.HasPrefix(arg, "-"):
			return agents.Account{}, nil, fmt.Errorf("unknown flag %q\n%s", arg, accountSetUsage)
		case selector == "":
			selector = arg
		default:
			field, value, ok := strings.Cut(arg, "=")
			if !ok || field == "" {
				return agents.Account{}, nil, fmt.Errorf("expected <field>=<value>, got %q\n%s", arg, accountSetUsage)
			}
			if value == "" {
				return agents.Account{}, nil, fmt.Errorf("%s has no value; use --unset %s for the provider default", field, field)
			}
			if err := add(field, &value); err != nil {
				return agents.Account{}, nil, err
			}
		}
	}
	if selector == "" || len(defaults) == 0 {
		return agents.Account{}, nil, fmt.Errorf("%s", accountSetUsage)
	}
	account, err := agents.ResolveAccount(store, selector)
	if err != nil {
		return agents.Account{}, nil, err
	}
	return account, defaults, nil
}

// printAccount shows one profile: describe output, then its launch defaults
// and the values each field accepts.
func printAccount(ctx context.Context, out io.Writer, registry *agents.Registry, account agents.Account) error {
	info := registry.DescribeAccount(ctx, account)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, value)
		}
	}
	row("name:", account.Name)
	row("provider:", string(account.Provider))
	row("id:", string(account.ID))
	row("auth:", info.AuthMode)
	row("identity:", info.Identity)
	row("plan:", info.Plan)
	if status, ok := info.Options["auth_status"].(string); ok {
		row("status:", status)
	}
	row("created:", account.CreatedAt.Format(time.RFC3339))
	row("updated:", account.UpdatedAt.Format(time.RFC3339))
	for _, warning := range info.Warnings {
		row("warning:", warning)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	provider, err := registry.Get(account.Provider)
	if err != nil {
		return nil
	}
	editor, ok := provider.(agents.ProfileEditor)
	if !ok {
		return nil
	}
	values := editor.ProfileDefaults(account)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Defaults (bonsai agent account set):")
	tw = tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  FIELD\tVALUE\tVALUES")
	for _, field := range editor.ProfileFields() {
		value := values[field.ID]
		if value == "" {
			value = "-"
		}
		choices := strings.Join(agents.FieldValues(field), ", ")
		if field.Kind == agents.LaunchFieldModel {
			choices = "any model name"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", field.ID, value, choices)
	}
	return tw.Flush()
}

const accountAddUsage = "usage: bonsai agent account add <provider> <name> [--auth <mode>] [--token-stdin] [--no-seed] [--seed-from <dir>]"

// maxStdinToken bounds --token-stdin input; real tokens are far shorter.
const maxStdinToken = 8 << 10

// parseAccountAddArgs parses `account add` arguments. Options a provider does not
// support are rejected by that provider's setup, not here.
func parseAccountAddArgs(args []string, in io.Reader) (agents.ProviderID, string, agents.SetupOptions, error) {
	var options agents.SetupOptions
	var positional []string
	tokenStdin := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, value, hasValue := strings.Cut(arg, "=")
		takeValue := func() (string, error) {
			if hasValue {
				if value == "" {
					return "", fmt.Errorf("%s requires a value", flag)
				}
				return value, nil
			}
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				return "", fmt.Errorf("%s requires a value", flag)
			}
			i++
			return args[i], nil
		}
		var err error
		switch flag {
		case "--auth":
			options.AuthMode, err = takeValue()
		case "--seed-from":
			options.SeedFrom, err = takeValue()
		case "--token-stdin", "--no-seed":
			if hasValue {
				return "", "", agents.SetupOptions{}, fmt.Errorf("%s does not take a value", flag)
			}
			if flag == "--no-seed" {
				noSeed := false
				options.Seed = &noSeed
			} else {
				tokenStdin = true
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return "", "", agents.SetupOptions{}, fmt.Errorf("unknown flag %q\n%s", arg, accountAddUsage)
			}
			positional = append(positional, arg)
		}
		if err != nil {
			return "", "", agents.SetupOptions{}, err
		}
	}
	if len(positional) != 2 {
		return "", "", agents.SetupOptions{}, fmt.Errorf("%s", accountAddUsage)
	}
	if options.Seed != nil && options.SeedFrom != "" {
		return "", "", agents.SetupOptions{}, fmt.Errorf("--no-seed and --seed-from cannot be combined")
	}
	if tokenStdin {
		secret, err := readStdinToken(in)
		if err != nil {
			return "", "", agents.SetupOptions{}, err
		}
		options.Secret = secret
	}
	return agents.ProviderID(positional[0]), positional[1], options, nil
}

// readStdinToken reads a --token-stdin secret. Errors never carry the input.
func readStdinToken(in io.Reader) (io.Reader, error) {
	if in == nil {
		return nil, fmt.Errorf("--token-stdin: no token on standard input")
	}
	data, err := io.ReadAll(io.LimitReader(in, maxStdinToken+1))
	if err != nil {
		return nil, fmt.Errorf("--token-stdin: cannot read standard input")
	}
	if len(data) > maxStdinToken {
		return nil, fmt.Errorf("--token-stdin: input too large")
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return nil, fmt.Errorf("--token-stdin: no token on standard input")
	}
	return strings.NewReader(token), nil
}

const accountLoginUsage = "usage: bonsai agent account login <account> [--auth <mode>] [--token-stdin]"

// parseAccountLoginArgs parses `account login` arguments. Without --auth the
// profile keeps its sign-in mode.
func parseAccountLoginArgs(args []string, in io.Reader) (string, agents.SetupOptions, error) {
	var options agents.SetupOptions
	var positional []string
	tokenStdin := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, value, hasValue := strings.Cut(arg, "=")
		switch flag {
		case "--auth":
			switch {
			case hasValue && value != "":
				options.AuthMode = value
			case !hasValue && i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "-"):
				i++
				options.AuthMode = args[i]
			default:
				return "", agents.SetupOptions{}, fmt.Errorf("--auth requires a value")
			}
		case "--token-stdin":
			if hasValue {
				return "", agents.SetupOptions{}, fmt.Errorf("--token-stdin does not take a value")
			}
			tokenStdin = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", agents.SetupOptions{}, fmt.Errorf("unknown flag %q\n%s", arg, accountLoginUsage)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return "", agents.SetupOptions{}, fmt.Errorf("%s", accountLoginUsage)
	}
	if tokenStdin {
		secret, err := readStdinToken(in)
		if err != nil {
			return "", agents.SetupOptions{}, err
		}
		options.Secret = secret
	}
	return positional[0], options, nil
}

// printSetupWarnings prints setup warnings where setup used to print them.
func printSetupWarnings(out io.Writer, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(out, "warning: %s\n", warning)
	}
}

func cmdAgentRun(ctx context.Context, runtime *agentRuntime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bonsai agent run <account> [-- <provider-args...>]")
	}
	account, err := agents.ResolveAccount(runtime.accounts, args[0])
	if err != nil {
		return err
	}
	var providerArgs []string
	if len(args) > 1 {
		if args[1] != "--" {
			return fmt.Errorf("provider arguments must follow --")
		}
		providerArgs = append([]string(nil), args[2:]...)
	}
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	return runtime.sessionService.RunForeground(ctx, account.ID, workDir, providerArgs)
}

func cmdAgentUsage(ctx context.Context, runtime *agentRuntime, args []string, out, errOut io.Writer) error {
	var selector string
	refresh := false
	var view usageView
	for _, arg := range args {
		switch arg {
		case "--refresh":
			refresh = true
		case "--claude-gpt":
			view.claudeGPT = true
		case "-h", "--help":
			fmt.Fprintln(out, "usage: bonsai agent usage [<account>] [--refresh] [--claude-gpt]")
			return nil
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown usage flag %q", arg)
			}
			if selector != "" {
				return fmt.Errorf("usage: bonsai agent usage [<account>] [--refresh] [--claude-gpt]")
			}
			selector = arg
		}
	}
	opts := agents.UsageOptions{Refresh: refresh}
	if selector != "" {
		account, err := agents.ResolveAccount(runtime.accounts, selector)
		if err != nil {
			return err
		}
		snapshot, err := runtime.usageService.Account(ctx, account.ID, opts)
		if errors.Is(err, agents.ErrUsageUnsupported) {
			// Expected for some profiles (long-lived tokens, macOS): show it, do not fail.
			printUsageDashboard(out, []agents.AccountUsageResult{{Account: account, Error: err}}, view)
			return nil
		}
		if err != nil {
			return err
		}
		printUsageDashboard(out, []agents.AccountUsageResult{{Account: account, Usage: &snapshot, Headline: runtime.usageService.Headline(snapshot)}}, view)
		return nil
	}

	results := runtime.usageService.All(ctx, opts)
	var errs []error
	for _, result := range results {
		if result.Error != nil && !errors.Is(result.Error, agents.ErrUsageUnsupported) {
			fmt.Fprintf(errOut, "%s/%s: %v\n", result.Account.Provider, result.Account.Name, result.Error)
			errs = append(errs, result.Error)
		}
	}
	printUsageDashboard(out, results, view)
	return errors.Join(errs...)
}

func printAgentUsage(w io.Writer) {
	fmt.Fprint(w, strings.TrimLeft(`
bonsai agent — provider account and session management

Usage:
  bonsai agent account add <provider> <name> [--auth <mode>] [--token-stdin]
                           [--no-seed] [--seed-from <dir>]
  bonsai agent account login <account> [--auth <mode>] [--token-stdin]
  bonsai agent account list
  bonsai agent account show <account>
  bonsai agent account set <account> <field>=<value>... [--unset <field>]...
  bonsai agent account rename <account> <new-name>
  bonsai agent account remove <account>
  bonsai agent run <account> [-- <provider-args...>]
  bonsai agent usage [<account>] [--refresh] [--claude-gpt]

Usage (Claude): reads the 5-hour and weekly utilization of login profiles from an
  undocumented Anthropic endpoint, so it may stop working. It uses the profile's
  stored login and never refreshes it. Token profiles and macOS show "n/a".

Providers:
  antigravity  bonsai agent account add antigravity <name>
  claude       bonsai agent account add claude <name>                login (default)
               bonsai agent account add claude <name> --auth token   asks for a setup-token
               echo "$TOKEN" | bonsai agent account add claude <name> --token-stdin
               New Claude profiles are seeded from ~/.claude (settings, CLAUDE.md,
               agents, commands, skills, output styles, MCP servers); never
               credentials or history. Use --no-seed to skip, --seed-from <dir> to
               choose another source. Several sessions of one login profile can
               race on token refresh; token mode avoids that. Sign in again (or
               switch mode) with bonsai agent account login <name> [--auth token]
  codex        bonsai agent account add codex <name>                   ChatGPT login (default)
               bonsai agent account add codex <name> --auth device     device code (SSH, WSL)
               bonsai agent account add codex <name> --auth api-key    asks for an API key
               printenv OPENAI_API_KEY | bonsai agent account add codex <name> --token-stdin
               Each profile owns its own CODEX_HOME; Codex keeps the login in its
               auth.json. New profiles are seeded from ~/.codex (config.toml without
               logins, projects or credential providers, AGENTS.md, prompts, skills,
               rules); never auth.json, sessions or history. --no-seed and
               --seed-from <dir> work as for Claude. Sign in again (or switch
               mode) with bonsai agent account login <name> [--auth <mode>]

Sign in again: bonsai agent account login <account> runs the provider's own
  login for an existing profile (Antigravity: agy, then exit it). Running
  sessions keep going. --auth switches the mode; --token-stdin reads a key or
  token as for add.
`, "\n"))
}
