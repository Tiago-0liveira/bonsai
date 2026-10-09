package agents

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestBuildEnvironmentSetUnsetAndPreserve(t *testing.T) {
	base := []string{"KEEP=yes", "CHANGE=old", "REMOVE=gone"}
	got := BuildEnvironment(base, map[string]string{"CHANGE": "new", "ADDED": "ok"}, []string{"REMOVE"})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"KEEP=yes", "CHANGE=new", "ADDED=ok"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment missing %q: %v", want, got)
		}
	}
	key := "REMOVE="
	if runtime.GOOS == "windows" {
		key = "REMOVE="
	}
	if strings.Contains(strings.ToUpper(joined), key) {
		t.Fatalf("environment retained removed key: %v", got)
	}
}

func setEnvCaseInsensitive(t *testing.T, value bool) {
	t.Helper()
	old := envCaseInsensitive
	envCaseInsensitive = value
	t.Cleanup(func() { envCaseInsensitive = old })
}

func TestBuildEnvironmentUnsetPrefixes(t *testing.T) {
	setEnvCaseInsensitive(t, false)
	base := []string{
		"PATH=/bin", "GH_TOKEN=gh", "HOME=/home/u", "MYCLAUDE=keep",
		"CLAUDECODE=1", "CLAUDE_X=x", "CLAUDE_CONFIG_DIR=/old", "ANTHROPIC_API_KEY=secret",
	}
	got := buildEnvironment(base, nil, nil, []string{"CLAUDE", "ANTHROPIC_"})
	want := []string{"GH_TOKEN=gh", "HOME=/home/u", "MYCLAUDE=keep", "PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestBuildEnvironmentSetOverridesUnsetPrefix(t *testing.T) {
	setEnvCaseInsensitive(t, false)
	base := []string{"CLAUDE_CONFIG_DIR=/old", "CLAUDE_X=x", "PATH=/bin"}
	got := buildEnvironment(base, map[string]string{"CLAUDE_CONFIG_DIR": "/profile"}, nil, []string{"CLAUDE"})
	want := []string{"CLAUDE_CONFIG_DIR=/profile", "PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestBuildEnvironmentUnsetPrefixCaseSensitivity(t *testing.T) {
	base := []string{"CLAUDE_UPPER=1", "claude_lower=2", "Claude_Mixed=3", "PATH=/bin"}

	t.Run("insensitive", func(t *testing.T) {
		setEnvCaseInsensitive(t, true)
		got := buildEnvironment(base, nil, nil, []string{"CLAUDE"})
		want := []string{"PATH=/bin"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("environment = %v, want %v", got, want)
		}
	})
	t.Run("sensitive", func(t *testing.T) {
		setEnvCaseInsensitive(t, false)
		got := buildEnvironment(base, nil, nil, []string{"CLAUDE"})
		want := []string{"Claude_Mixed=3", "PATH=/bin", "claude_lower=2"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("environment = %v, want %v", got, want)
		}
	})
}

func TestBuildEnvironmentEmptyPrefixRemovesNothing(t *testing.T) {
	setEnvCaseInsensitive(t, false)
	base := []string{"A=1", "B=2", "PATH=/bin"}
	got := buildEnvironment(base, nil, nil, []string{""})
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("environment = %v, want %v", got, base)
	}
}

func TestPreparedSessionEnvironmentCombinesSetUnsetAndPrefixes(t *testing.T) {
	setEnvCaseInsensitive(t, false)
	prepared := PreparedSession{
		EnvSet:           map[string]string{"CLAUDE_CONFIG_DIR": "/profile", "ADDED": "ok"},
		EnvUnset:         []string{"REMOVE"},
		EnvUnsetPrefixes: []string{"ANTHROPIC_", "CLAUDE"},
	}
	base := []string{
		"PATH=/bin", "REMOVE=gone", "ANTHROPIC_API_KEY=secret",
		"CLAUDECODE=1", "CLAUDE_CONFIG_DIR=/old", "KEEP=yes",
	}
	got := prepared.Environment(base)
	want := []string{"ADDED=ok", "CLAUDE_CONFIG_DIR=/profile", "KEEP=yes", "PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}
