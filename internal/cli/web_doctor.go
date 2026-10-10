package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

// doctor prints the same checks the setup screens show and exits 1 when any
// of them failed. It never changes anything.
func (w *webCLI) doctor() error {
	rootsPath, err := config.ProjectRootsPath()
	if err != nil {
		return err
	}
	var results []checks.Check
	cfg, _, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		results = append(results, checks.Check{
			ID: "settings", Title: "web settings", State: checks.Fail,
			Detail: err.Error(),
			Fix:    &checks.Fix{Command: "fix or delete " + w.configPath + ", then run bonsai web setup"},
		})
		cfg = config.DefaultWebConfig()
	}
	results = append(results, checks.Run(context.Background(), w.checksEnv(rootsPath), checks.Options{
		APIPort:     cfg.APIPort,
		UpdatesMode: cfg.Updates.Mode,
	})...)
	return w.printDoctor(results)
}

func doctorGlyph(state checks.State) string {
	switch state {
	case checks.OK:
		return "✓"
	case checks.Warn:
		return "!"
	case checks.Fail:
		return "✗"
	}
	return "–"
}

// printDoctor renders one line per check, a fix under every warning or
// failure, and a summary. The glyphs and words carry the state; there is no
// color to lose when the output is piped.
func (w *webCLI) printDoctor(results []checks.Check) error {
	fmt.Fprintln(w.out, "bonsai web doctor")
	width := 0
	for _, c := range results {
		if n := len([]rune(c.Title)); n > width {
			width = n
		}
	}
	for _, c := range results {
		fmt.Fprintf(w.out, "  %s %-*s  %s\n", doctorGlyph(c.State), width, c.Title, c.Detail)
		if c.Fix != nil && (c.State == checks.Warn || c.State == checks.Fail) {
			fmt.Fprintf(w.out, "    %-*s  fix  %s\n", width, "", c.Fix.Command)
		}
	}
	failed, warned := checks.Counts(results)
	fmt.Fprintln(w.out)
	switch {
	case failed+warned == 0:
		fmt.Fprintln(w.out, "✓ Everything bonsai web needs is in place.")
	default:
		var parts []string
		if failed > 0 {
			parts = append(parts, countWord(failed, "problem"))
		}
		if warned > 0 {
			parts = append(parts, countWord(warned, "warning"))
		}
		glyph := "!"
		if failed > 0 {
			glyph = "✗"
		}
		fmt.Fprintf(w.out, "%s %s\n", glyph, strings.Join(parts, ", "))
	}
	if failed > 0 {
		return &ExitError{Code: 1}
	}
	return nil
}

func countWord(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
