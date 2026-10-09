package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

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
		return fmt.Errorf("usage: bonsai agent account <add|list|rename|remove>")
	}
	switch args[0] {
	case "add":
		provider, name, options, err := parseAccountAddArgs(args[1:], in)
		if err != nil {
			return err
		}
		account, err := runtime.accountService.Setup(ctx, provider, name, options)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "added %s account %q (%s)\n", account.Provider, account.Name, account.ID)
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
		fmt.Fprintln(tw, "NAME\tPROVIDER\tAUTH\tIDENTITY")
		for _, account := range accounts {
			info := runtime.accountService.Registry.DescribeAccount(ctx, account)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", account.Name, account.Provider, info.AuthMode, info.Identity)
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
		warnings, err := runtime.accountService.Remove(ctx, account.ID)
		for _, warning := range warnings {
			fmt.Fprintf(errOut, "warning: %s\n", warning)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %s account %q\n", account.Provider, account.Name)
		return nil
	default:
		return fmt.Errorf("unknown agent account command %q", args[0])
	}
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
		if in == nil {
			return "", "", agents.SetupOptions{}, fmt.Errorf("--token-stdin: no token on standard input")
		}
		data, err := io.ReadAll(io.LimitReader(in, maxStdinToken+1))
		if err != nil {
			return "", "", agents.SetupOptions{}, fmt.Errorf("--token-stdin: cannot read standard input")
		}
		if len(data) > maxStdinToken {
			return "", "", agents.SetupOptions{}, fmt.Errorf("--token-stdin: input too large")
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", "", agents.SetupOptions{}, fmt.Errorf("--token-stdin: no token on standard input")
		}
		options.Secret = strings.NewReader(token)
	}
	return agents.ProviderID(positional[0]), positional[1], options, nil
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
	for _, arg := range args {
		switch arg {
		case "--refresh":
			refresh = true
		case "-h", "--help":
			fmt.Fprintln(out, "usage: bonsai agent usage [<account>] [--refresh]")
			return nil
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown usage flag %q", arg)
			}
			if selector != "" {
				return fmt.Errorf("usage: bonsai agent usage [<account>] [--refresh]")
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
			printUsageDashboard(out, []agents.AccountUsageResult{{Account: account, Error: err}})
			return nil
		}
		if err != nil {
			return err
		}
		printUsageDashboard(out, []agents.AccountUsageResult{{Account: account, Usage: &snapshot}})
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
	printUsageDashboard(out, results)
	return errors.Join(errs...)
}

func printAgentUsage(w io.Writer) {
	fmt.Fprint(w, strings.TrimLeft(`
bonsai agent — provider account and session management

Usage:
  bonsai agent account add <provider> <name> [--auth <mode>] [--token-stdin]
                           [--no-seed] [--seed-from <dir>]
  bonsai agent account list
  bonsai agent account rename <account> <new-name>
  bonsai agent account remove <account>
  bonsai agent run <account> [-- <provider-args...>]
  bonsai agent usage [<account>] [--refresh]

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
               race on token refresh; token mode avoids that.
`, "\n"))
}
