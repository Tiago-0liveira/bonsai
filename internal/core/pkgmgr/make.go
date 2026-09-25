package pkgmgr

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type makeProvider struct{}

func (makeProvider) ID() string { return "make" }

type makeDetection struct {
	Makefile string
}

func (makeProvider) Detect(ctx Context) (Detection, error) {
	root, path := findUp(ctx.Location.InputDir, "Makefile", "makefile", "GNUmakefile")
	if root == "" {
		return Detection{}, nil
	}
	return Detection{Applicable: true, ID: "make", Name: "make", Root: root, Data: makeDetection{Makefile: path}}, nil
}

func (makeProvider) Commands(_ Context, detection Detection) ([]Command, error) {
	d, ok := detection.Data.(makeDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid make detection data")
	}
	targets, err := parseMakefile(d.Makefile)
	if err != nil {
		return nil, err
	}
	commands := make([]Command, 0, len(targets))
	for _, target := range targets {
		desc := "Make target"
		if target.Phony {
			desc = "Make phony target"
		}
		commands = append(commands, Command{
			ID:          "make:target:" + target.Name,
			Name:        target.Name,
			Description: desc,
			Provider:    detection.ID,
			Kind:        CommandProject,
			Invocation:  InvocationSpec{Program: "make", Prefix: []string{target.Name}, WorkingDir: detection.Root},
			Source:      Source{Kind: "makefile", File: d.Makefile, Line: target.Line, Pointer: target.Name},
			Confidence:  ConfidenceHigh,
		})
	}
	return commands, nil
}

func (makeProvider) FingerprintInputs(_ Context, detection Detection) ([]FingerprintInput, error) {
	d, ok := detection.Data.(makeDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid make detection data")
	}
	return fileInputs("make", "", detection.Root, []string{d.Makefile})
}

type makeTarget struct {
	Name  string
	Line  int
	Phony bool
}

func parseMakefile(path string) ([]makeTarget, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var found []makeTarget
	phony := map[string]bool{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
			continue
		}
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ".PHONY:") {
			for _, name := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, ".PHONY:"))) {
				phony[name] = true
			}
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		left := strings.TrimSpace(line[:colon])
		if left == "" || strings.Contains(left, "=") {
			continue
		}
		for _, name := range strings.Fields(left) {
			if strings.HasPrefix(name, ".") || strings.Contains(name, "%") || strings.ContainsAny(name, "$(){}") || seen[name] {
				continue
			}
			seen[name] = true
			found = append(found, makeTarget{Name: name, Line: lineNo})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for i := range found {
		found[i].Phony = phony[found[i].Name]
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Line == found[j].Line {
			return found[i].Name < found[j].Name
		}
		return found[i].Line < found[j].Line
	})
	return found, nil
}

func makefileName(path string) string { return filepath.Base(path) }
