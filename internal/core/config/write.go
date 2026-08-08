package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

// FileFor returns the path of the .bonsai.yaml LoadFor would read: the current
// worktree's own file when it carries one, else the main repo's. The file may
// not exist yet (writes create it).
func FileFor(mainRoot string) string {
	dir := mainRoot
	if cwd, err := os.Getwd(); err == nil {
		if root, err := git.RepoRoot(cwd); err == nil {
			if _, statErr := os.Stat(filepath.Join(root, ".bonsai.yaml")); statErr == nil {
				dir = root
			}
		}
	}
	return filepath.Join(dir, ".bonsai.yaml")
}

// readDoc parses file into a yaml.Node tree so writes can round-trip comments
// and layout. A missing or empty file yields a fresh empty document.
func readDoc(file string) (*yaml.Node, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyDoc(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(file), err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return emptyDoc(), nil
	}
	return &root, nil
}

func emptyDoc() *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
}

// writeDoc marshals root and replaces file atomically (tmp + rename).
func writeDoc(file string, root *yaml.Node) error {
	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".bonsai.yaml.*")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmpName, file); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// mapValue returns the value node for key inside mapping node m, or nil when
// absent. With create, a missing pair is appended with an empty mapping value.
func mapValue(m *yaml.Node, key string, create bool) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	if !create {
		return nil
	}
	v := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
	return v
}

// walk resolves the dotted keyPath to its parent mapping and final segment.
// ok is false when the path does not exist (with create=false) or cannot be
// extended (with create=true, e.g. a scalar sits mid-path).
func walk(root *yaml.Node, keyPath string, create bool) (parent *yaml.Node, last string, ok bool, err error) {
	segs := strings.Split(keyPath, ".")
	cur := root.Content[0]
	for _, seg := range segs[:len(segs)-1] {
		next := mapValue(cur, seg, create)
		if next == nil {
			return nil, "", false, nil
		}
		if next.Kind != yaml.MappingNode {
			return nil, "", false, fmt.Errorf("config key %q is not a mapping", seg)
		}
		cur = next
	}
	return cur, segs[len(segs)-1], true, nil
}

// setPair replaces (or appends) the value of key in mapping m, keeping the key
// node — and any comments attached to it — intact.
func setPair(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

// scalarNode builds a scalar for value; "true"/"false" are tagged as booleans
// so they round-trip as bools when read back.
func scalarNode(value string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	if value == "true" || value == "false" {
		n.Tag = "!!bool"
	}
	return n
}

// Set writes a scalar value at the dotted keyPath (e.g. "notifications.ci"),
// creating intermediate mappings as needed. Comments and unrelated keys in the
// file are preserved. A missing file is created.
func Set(file, keyPath, value string) error {
	root, err := readDoc(file)
	if err != nil {
		return err
	}
	parent, last, ok, err := walk(root, keyPath, true)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("config key %q not found", keyPath)
	}
	setPair(parent, last, scalarNode(value))
	return writeDoc(file, root)
}

// ListSet replaces the sequence at keyPath with values (creating it when
// absent). An empty values slice writes an empty list.
func ListSet(file, keyPath string, values []string) error {
	root, err := readDoc(file)
	if err != nil {
		return err
	}
	parent, last, ok, err := walk(root, keyPath, true)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("config key %q not found", keyPath)
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, v := range values {
		seq.Content = append(seq.Content, scalarNode(v))
	}
	setPair(parent, last, seq)
	return writeDoc(file, root)
}

// Unset removes the key at keyPath. Missing keys and files are no-ops.
func Unset(file, keyPath string) error {
	root, err := readDoc(file)
	if err != nil {
		return err
	}
	parent, last, ok, err := walk(root, keyPath, false)
	if err != nil {
		return err
	}
	if !ok || parent == nil {
		return nil
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == last {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return writeDoc(file, root)
		}
	}
	return nil
}
