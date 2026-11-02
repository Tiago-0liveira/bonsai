package launchform

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestAccountsDefaultsFormatterLegacyAndCompact(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			field    agents.LaunchField
			value    string
			want     string
			unlisted bool
		}{
			{"unlisted select", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldSelect}, "retired", "retired (unlisted)", true},
			{"unlisted model", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldModel}, "retired-model", "retired-model (unlisted)", true},
			{"listed label", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: "known", Label: "Known choice"}}}, "known", "Known choice", false},
			{"inherited empty", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldModel}, "", agents.ProviderDefaultLabel, false},
			{"read only", agents.LaunchField{ID: "setting", Kind: "future-kind"}, "opaque", "opaque", false},
			{"unlisted toggle", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldToggle}, "retired", "retired (unlisted)", true},
			{"toggle on", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldToggle}, "true", "On", false},
			{"toggle off", agents.LaunchField{ID: "setting", Kind: agents.LaunchFieldToggle}, "false", "Off", false},
		} {
			t.Run(fmt.Sprintf("%s/ascii=%v", tc.name, ascii), func(t *testing.T) {
				tc.field.Label = "Setting"
				f := New([]agents.LaunchField{tc.field}, nil, map[string]string{"setting": tc.value}, ascii)
				compact, _, _ := f.CompactLines(100)
				for _, out := range []string{ansi.Strip(f.View(100, 12)), ansi.Strip(strings.Join(compact, "\n"))} {
					if !strings.Contains(out, tc.want) || strings.Contains(out, "(unlisted)") != tc.unlisted {
						t.Errorf("format lacks %q or mislabels validity:\n%s", tc.want, out)
					}
				}
				if f.Dirty() || len(f.Changes()) != 0 {
					t.Fatal("formatting rewrote a stored value")
				}
			})
		}
	}
}
