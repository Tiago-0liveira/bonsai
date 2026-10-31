package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
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
	root, _, err := readDocSource(file)
	return root, err
}

// readDocSource retains the same bytes that determined the node positions.
func readDocSource(file string) (*yaml.Node, []byte, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyDoc(), data, nil
		}
		return nil, nil, fmt.Errorf("read config: %w", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", filepath.Base(file), err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return emptyDoc(), data, nil
	}
	return &root, data, nil
}

func emptyDoc() *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
}

// writeDoc encodes structural edits with the repo's usual indentation. This
// fallback can lose blank lines; scalar replacements use the original bytes.
func writeDoc(file string, root *yaml.Node) error {
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return writeConfigBytes(file, out.Bytes())
}

func writeConfigBytes(file string, out []byte) error {
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
	if err := gitstore.ReplaceFile(tmpName, file); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// spliceScalar handles single-line block scalars whose token can be identified safely.
// Multiline, tagged and anchored values fall back to encoding the document.
func spliceScalar(data []byte, old, value *yaml.Node) ([]byte, bool) {
	if old.Kind != yaml.ScalarNode || old.Anchor != "" || old.Line < 1 || old.Column < 1 || old.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 {
		return nil, false
	}
	start := 0
	for line := 1; line < old.Line; line++ {
		i := bytes.IndexByte(data[start:], '\n')
		if i < 0 {
			return nil, false
		}
		start += i + 1
	}
	// yaml.Node columns count characters, while the splice uses byte offsets.
	for col := 1; col < old.Column && start < len(data); col++ {
		_, size := utf8.DecodeRune(data[start:])
		start += size
	}
	end := start
	for end < len(data) && data[end] != '\n' && data[end] != '\r' {
		end++
	}
	token := data[start:end]
	if len(token) == 0 {
		return nil, false
	}
	if token[0] == '"' || token[0] == '\'' {
		quote := token[0]
		i := 1
		for ; i < len(token); i++ {
			if quote == '"' && token[i] == '\\' {
				i++
				continue
			}
			if token[i] == quote {
				if quote == '\'' && i+1 < len(token) && token[i+1] == quote {
					i++
					continue
				}
				break
			}
		}
		if i >= len(token) {
			return nil, false
		}
		rest := bytes.TrimSpace(token[i+1:])
		if len(rest) > 0 && rest[0] != '#' {
			return nil, false
		}
		token = token[:i+1]
	} else {
		for i, ch := range token {
			if ch == '#' && (i == 0 || token[i-1] == ' ' || token[i-1] == '\t') {
				token = token[:i]
				break
			}
		}
		token = bytes.TrimRight(token, " \t")
	}
	var parsed yaml.Node
	if err := yaml.Unmarshal(token, &parsed); err != nil || len(parsed.Content) != 1 {
		return nil, false
	}
	n := parsed.Content[0]
	if n.Kind != yaml.ScalarNode || n.Value != old.Value || n.Tag != old.Tag || n.Style != old.Style {
		return nil, false
	}
	replacement := *value
	replacement.Style = old.Style
	replacement.HeadComment, replacement.LineComment, replacement.FootComment = "", "", ""
	encoded, err := yaml.Marshal(&replacement)
	if err != nil {
		return nil, false
	}
	encoded = bytes.TrimSuffix(encoded, []byte("\n"))
	if bytes.ContainsAny(encoded, "\r\n") {
		return nil, false
	}
	out := make([]byte, 0, len(data)-len(token)+len(encoded))
	out = append(out, data[:start]...)
	out = append(out, encoded...)
	out = append(out, data[start+len(token):]...)
	return out, true
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
	root, data, err := readDocSource(file)
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
	next := scalarNode(value)
	if old := mapValue(parent, last, false); old != nil {
		// Standalone scalar encoding may emit commas that are unsafe in flow maps.
		if parent.Style&yaml.FlowStyle == 0 {
			if out, ok := spliceScalar(data, old, next); ok {
				return writeConfigBytes(file, out)
			}
		}
		next.HeadComment, next.LineComment, next.FootComment = old.HeadComment, old.LineComment, old.FootComment
		if old.Kind == yaml.ScalarNode {
			next.Style = old.Style
		}
	}
	setPair(parent, last, next)
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
