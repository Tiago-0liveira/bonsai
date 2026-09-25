package pkgmgr

import (
	"fmt"
	"path/filepath"
	"strings"
)

type detectedProvider struct {
	provider  Provider
	detection Detection
	ctx       Context
}

var defaultProviders = []Provider{nodeProvider{}, pythonProvider{}, goProvider{}, cargoProvider{}, makeProvider{}}

// Discover finds every applicable provider and returns a normalized command catalog.
func Discover(dir string, opts Options) (*Project, error) {
	loc, err := resolveLocation(dir)
	if err != nil {
		return nil, err
	}
	var detected []detectedProvider
	var warnings []Warning
	var firstErr error
	seenDetection := map[string]bool{}
	zeroDepth := 0
	for _, candidate := range projectSearchDirs(loc.InputDir, opts.searchDepth()) {
		candidateLoc := loc
		candidateLoc.InputDir = candidate
		candidateLoc.ProjectRoot = candidate
		detectOpts := opts
		detectOpts.SearchDepth = &zeroDepth
		detectCtx := Context{Location: candidateLoc, Options: detectOpts}
		commandCtx := Context{Location: candidateLoc, Options: opts}
		for _, provider := range defaultProviders {
			detection, detectErr := provider.Detect(detectCtx)
			if detectErr != nil {
				if firstErr == nil {
					firstErr = detectErr
				}
				warnings = append(warnings, Warning{Provider: provider.ID(), Message: detectErr.Error()})
				continue
			}
			if !detection.Applicable {
				continue
			}
			identityRoot := detection.Root
			if detection.ID == "cargo" && detection.WorkspaceRoot != "" {
				identityRoot = detection.WorkspaceRoot
			}
			key := detection.ID + "\x00" + filepath.Clean(identityRoot)
			if seenDetection[key] {
				continue
			}
			seenDetection[key] = true
			detected = append(detected, detectedProvider{provider: provider, detection: detection, ctx: commandCtx})
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
	providerInfos := make([]ProviderInfo, 0, len(detected))
	providerIDs := make([]string, 0, len(detected))
	var fingerprintInputs []FingerprintInput
	for _, item := range detected {
		d := item.detection
		providerInfos = append(providerInfos, ProviderInfo{ID: d.ID, Name: d.Name, Root: d.Root, WorkspaceRoot: d.WorkspaceRoot})
		providerIDs = append(providerIDs, d.ID)
		providerCtx := item.ctx
		providerCtx.Location.ProjectRoot = d.Root
		providerCtx.Location.WorkspaceRoot = d.WorkspaceRoot
		inputs, fpErr := item.provider.FingerprintInputs(providerCtx, d)
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

	providerFamilyCounts := map[string]int{}
	for _, item := range detected {
		providerFamilyCounts[item.provider.ID()]++
	}
	cacheSafe := true
	for _, count := range providerFamilyCounts {
		if count > 1 {
			cacheSafe = false
			break
		}
	}

	if opts.UseCache && cacheSafe && !opts.Refresh {
		if cached, ok := loadCache(fingerprint); ok {
			return rebaseCachedProject(*cached, loc, detected), nil
		}
	}

	var groups [][]Command
	for _, item := range detected {
		providerCtx := item.ctx
		providerCtx.Location.ProjectRoot = item.detection.Root
		providerCtx.Location.WorkspaceRoot = item.detection.WorkspaceRoot
		commands, commandErr := item.provider.Commands(providerCtx, item.detection)
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
			if providerFamilyCounts[item.provider.ID()] > 1 {
				scope := projectScope(loc, item.detection.Root)
				commands[i].ID = commands[i].ID + "@" + scope
			}
		}
		groups = append(groups, commands)
	}
	commands := mergeCommands(groups...)
	overrideRoot := loc.ProjectRoot
	if overridePath != "" {
		overrideRoot = filepath.Dir(overridePath)
	}
	commands, err = applyOverrides(commands, overrides, overridePath, overrideRoot)
	if err != nil {
		return nil, err
	}
	commands = mergeCommands(commands)

	project := &Project{Location: loc, Providers: providerInfos, Commands: commands, Fingerprint: fingerprint, Warnings: warnings}
	if opts.UseCache && cacheSafe {
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
			oldRoot = oldLocation.ProjectRoot
			if cmd.Source.File != "" {
				oldRoot = filepath.Dir(cmd.Source.File)
			}
			newRoot = loc.ProjectRoot
			if path := findOverridePath(loc); path != "" {
				newRoot = filepath.Dir(path)
			}
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


func projectScope(loc Location, root string) string {
	base := loc.RepositoryRoot
	if base == "" {
		base = loc.InputDir
	}
	if rel, err := filepath.Rel(base, root); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if rel == "." {
			return "."
		}
		return filepath.ToSlash(rel)
	}
	return "project"
}
