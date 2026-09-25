package pkgmgr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const discoverySchemaVersion = 1

var providerVersions = map[string]string{
	"node":  "1",
	"cargo": "1",
	"make":  "1",
}

func fileInputs(prefix, workspaceRoot, projectRoot string, paths []string) ([]FingerprintInput, error) {
	base := workspaceRoot
	if base == "" {
		base = projectRoot
	}
	seen := map[string]bool{}
	inputs := make([]FingerprintInput, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(path)
		if rel, err := filepath.Rel(base, path); err == nil && rel != ".." && !(len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator)) {
			name = filepath.ToSlash(rel)
		}
		inputs = append(inputs, FingerprintInput{Name: prefix + "/" + name, Content: data})
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Name < inputs[j].Name })
	return inputs, nil
}

func computeFingerprint(providerIDs []string, inputs []FingerprintInput) string {
	h := sha256.New()
	fmt.Fprintf(h, "schema=%d\n", discoverySchemaVersion)
	ids := append([]string(nil), providerIDs...)
	sort.Strings(ids)
	for _, id := range ids {
		family := id
		if len(id) >= 5 && id[:5] == "node:" {
			family = "node"
		}
		fmt.Fprintf(h, "provider=%s@%s\n", id, providerVersions[family])
	}
	sorted := append([]FingerprintInput(nil), inputs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return bytes.Compare(sorted[i].Content, sorted[j].Content) < 0
	})
	for _, input := range sorted {
		sum := sha256.Sum256(input.Content)
		fmt.Fprintf(h, "input=%s:%s\n", input.Name, hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil))
}
