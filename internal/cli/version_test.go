package cli

import (
	"bytes"
	"github.com/Tiago-0liveira/bonsai/internal/version"
	"strings"
	"testing"
)

func TestVersionOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, arg := range []string{"version", "-v", "--version"} {
		var out, errOut bytes.Buffer
		if err := Run([]string{arg}, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.String()) != "bonsai "+version.String() {
			t.Fatal(out.String())
		}
	}
}
