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

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
)

type agentRuntime struct {
	accounts       agents.AccountStore
	accountService *agents.AccountService
	sessionService *agents.SessionService
	usageService   *agents.UsageService
}

func newAgentRuntime(in io.Reader, out, errOut io.Writer) (*agentRuntime, error) {
	accounts, err := agents.NewFileAccountStore("")
	if err != nil {
		return nil, err
	}
	sessions, err := agents.NewFileSessionStore("")
	if err != nil {
		return nil, err
	}
	cache, err := agents.NewFileUsageCache("")
	if err != nil {
		return nil, err
	}
	launcher := agents.NewForegroundLauncher(in, out, errOut)
	registry := agents.NewRegistry()
	if err := registry.Register(antigravity.New(accounts, sessions, launcher)); err != nil {
		return nil, err
	}
	return &agentRuntime{
		accounts: accounts,
		accountService: &agents.AccountService{
			Store: accounts, Sessions: sessions, Registry: registry,
			Launcher: launcher, Cache: cache,
		},
		sessionService: &agents.SessionService{
			Accounts: accounts, Sessions: sessions, Registry: registry, Launcher: launcher,
		},
		usageService: &agents.UsageService{
			Accounts: accounts, Sessions: sessions, Registry: registry,
			Cache: cache, TTL: time.Minute, WorkerLimit: 8,
		},
	}, nil
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
		return cmdAgentAccount(ctx, runtime, args[1:], out)
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

func cmdAgentAccount(ctx context.Context, runtime *agentRuntime, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bonsai agent account <add|list|rename|remove>")
	}
	switch args[0] {
	case "add":
		if len(args) != 3 {
			return fmt.Errorf("usage: bonsai agent account add <provider> <name>")
		}
		account, err := runtime.accountService.Setup(ctx, agents.ProviderID(args[1]), args[2])
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
		fmt.Fprintln(tw, "NAME\tPROVIDER")
		for _, account := range accounts {
			fmt.Fprintf(tw, "%s\t%s\n", account.Name, account.Provider)
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
		if err := runtime.accountService.Remove(ctx, account.ID); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %s account %q\n", account.Provider, account.Name)
		return nil
	default:
		return fmt.Errorf("unknown agent account command %q", args[0])
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
		if err != nil {
			return err
		}
		printUsageSnapshot(out, account, snapshot)
		return nil
	}

	results := runtime.usageService.All(ctx, opts)
	var errs []error
	for _, result := range results {
		if result.Error != nil {
			fmt.Fprintf(errOut, "%s/%s: %v\n", result.Account.Provider, result.Account.Name, result.Error)
			errs = append(errs, result.Error)
			continue
		}
		if result.Usage != nil {
			printUsageSnapshot(out, result.Account, *result.Usage)
		}
	}
	return errors.Join(errs...)
}

func printUsageSnapshot(out io.Writer, account agents.Account, snapshot agents.UsageSnapshot) {
	fmt.Fprintf(out, "%s (%s)\n", account.Name, account.Provider)
	if len(snapshot.Limits) == 0 {
		fmt.Fprintln(out, "  no usage limits reported")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  GROUP\tWINDOW\tREMAINING\tRESET")
	for _, limit := range snapshot.Limits {
		remaining := "-"
		if limit.RemainingFraction != nil {
			remaining = fmt.Sprintf("%.0f%%", *limit.RemainingFraction*100)
		}
		reset := "-"
		if limit.ResetsAt != nil {
			reset = limit.ResetsAt.Local().Format(time.RFC3339)
		}
		group := limit.Group
		if group == "" {
			group = limit.Label
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", group, limit.Window, remaining, reset)
	}
	_ = tw.Flush()
}

func printAgentUsage(w io.Writer) {
	fmt.Fprint(w, strings.TrimLeft(`
bonsai agent — provider account and session management

Usage:
  bonsai agent account add <provider> <name>
  bonsai agent account list
  bonsai agent account rename <account> <new-name>
  bonsai agent account remove <account>
  bonsai agent run <account> [-- <provider-args...>]
  bonsai agent usage [<account>] [--refresh]
`, "\n"))
}
