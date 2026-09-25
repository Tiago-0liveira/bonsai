package pkgmgr

import (
	"fmt"
	"path/filepath"
	"strings"
)

type detectedProvider struct {
	provider  Provider
	detection Detection
}

var defaultProviders = []Provider{nodeProvider{}, cargoProvider{}, makeProvider{}}

// Discover finds every applicable provider and returns a normalized command catalog.
func Discover(dir string, opts Options) (*Project, error) {
	loc, err := resolveLocation(dir)
	if err != nil {
		return nil, err
	}
	ctx := Context{Location: loc, Options: opts}
	var detected []detectedProvider
	var warnings []Warning
	var firstErr error
	for _, provider := range defaultProviders {
		detection, detectErr := provider.Detect(ctx)
		if detectErr != nil {
			if firstErr == nil {
				firstErr = detectErr
			}
			warnings = append(warnings, Warning{Provider: provider.ID(), Message: detectErr.Error()})
			continue
		}
		if detection.Applicable {
			detected = append(detected, detectedProvider{provider: provider, detection: detection})
		}
	}
	if len(detected) == 0 && firstErr != nil {
		return nil, firstErr
	}

	var roots, workspaces []string
	for _, item := range detected {
		roots = append(roots, item.detection.Root)
		workspaces = append(workspaces, item.detection.WorkspaceRoot)
	}
	if root := nearestRoot(loc.InputDir, roots...); root != "" {
		loc.ProjectRoot = root
	}
	if workspace := nearestRoot(loc.InputDir, workspaces...); workspace != "" {
		loc.WorkspaceRoot = workspace
	}
	ctx.Location = loc

	providerInfos := make([]ProviderInfo, 0, len(detected))
	providerIDs := make([]string, 0, len(detected))
	var fingerprintInputs []FingerprintInput
	for _, item := range detected {
		d := item.detection
		providerInfos = append(providerInfos, ProviderInfo{ID: d.ID, Name: d.Name, Root: d.Root, WorkspaceRoot: d.WorkspaceRoot})
		providerIDs = append(providerIDs, d.ID)
		inputs, fpErr := item.provider.FingerprintInputs(ctx, d)
		if fpErr != nil {
			warnings = append(warnings, Warning{Provider: d.ID, Message: fpErr.Error()})
			continue
		}
		fingerprintInputs = append(fingerprintInputs, inputs...)
	}

	overrides, overridePath, overrideFingerprint, overrideErr := loadOverrides(loc)
	if overrideErr != nil {
		return nil, fmt.Errorf("pkgmgr: read overrides: %w", overrideErr)
	}
	if len(overrideFingerprint) > 0 {
		fingerprintInputs = append(fingerprintInputs, FingerprintInput{Name: "override/.bonsai.yaml#pkgmgr", Content: overrideFingerprint})
	}
	fingerprint := computeFingerprint(providerIDs, fingerprintInputs)

	if opts.UseCache && !opts.Refresh {
		if cached, ok := loadCache(fingerprint); ok {
			return rebaseCachedProject(*cached, loc, detected), nil
		}
	}

	var groups [][]Command
	for _, item := range detected {
		commands, commandErr := item.provider.Commands(ctx, item.detection)
		if commandErr != nil {
			warnings = append(warnings, Warning{Provider: item.detection.ID, Message: commandErr.Error()})
			continue
		}
		for i := range commands {
			if commands[i].Provider == "" {
				commands[i].Provider = item.detection.ID
			}
			if commands[i].Invocation.WorkingDir == "" {
				commands[i].Invocation.WorkingDir = item.detection.Root
			}
		}
		groups = append(groups, commands)
	}
	commands := mergeCommands(groups...)
	commands, err = applyOverrides(commands, overrides, overridePath, loc.ProjectRoot)
	if err != nil {
		return nil, err
	}
	commands = mergeCommands(commands)

	project := &Project{Location: loc, Providers: providerInfos, Commands: commands, Fingerprint: fingerprint, Warnings: warnings}
	if opts.UseCache {
		_ = storeCache(*project)
	}
	return project, nil
}

func rebaseCachedProject(project Project, loc Location, detected []detectedProvider) *Project {
	oldLocation := project.Location
	oldRoots := map[string]string{}
	for _, info := range project.Providers {
		oldRoots[info.ID] = info.Root
	}
	newRoots := map[string]string{}
	workspaces := map[string]string{}
	for _, item := range detected {
		newRoots[item.detection.ID] = item.detection.Root
		workspaces[item.detection.ID] = item.detection.WorkspaceRoot
	}
	project.Location = loc
	for i := range project.Providers {
		if root := newRoots[project.Providers[i].ID]; root != "" {
			project.Providers[i].Root = root
			project.Providers[i].WorkspaceRoot = workspaces[project.Providers[i].ID]
		}
	}
	for i := range project.Commands {
		cmd := &project.Commands[i]
		oldRoot, newRoot := oldRoots[cmd.Provider], newRoots[cmd.Provider]
		if cmd.Provider == "override" {
			oldRoot, newRoot = oldLocation.ProjectRoot, loc.ProjectRoot
		}
		if newRoot == "" {
			continue
		}
		cmd.Invocation.WorkingDir = rebasePath(cmd.Invocation.WorkingDir, oldRoot, newRoot)
		cmd.Source.File = rebasePath(cmd.Source.File, oldRoot, newRoot)
		for j := range cmd.Args {
			cmd.Args[j].Source.File = rebasePath(cmd.Args[j].Source.File, oldRoot, newRoot)
		}
	}
	return &project
}

func rebasePath(path, oldRoot, newRoot string) string {
	if path == "" || oldRoot == "" || newRoot == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(oldRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.Join(newRoot, rel)
}
