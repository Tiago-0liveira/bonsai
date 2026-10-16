//go:build !windows

package agentsbackend

import (
	"context"
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"strings"
	"testing"
)

func TestUpdateDefaultsSanitizesProviderError(t *testing.T) {
	f := newFixture(t)
	f.provider.updateErr = errors.New("write " + secretPath + " token=sk-abc")
	value := "opus"
	err := f.acc.UpdateDefaults(context.Background(), f.account.ID, map[string]*string{"model": &value})
	if !errors.Is(err, agents.ErrInvalidProfile) || strings.Contains(err.Error(), secretPath) || strings.Contains(err.Error(), "sk-abc") {
		t.Fatalf("UpdateDefaults = %v", err)
	}
	err = f.acc.UpdateDefaults(context.Background(), "acct_missing", map[string]*string{"model": &value})
	if !errors.Is(err, agents.ErrAccountNotFound) {
		t.Fatalf("missing account = %v", err)
	}
}
