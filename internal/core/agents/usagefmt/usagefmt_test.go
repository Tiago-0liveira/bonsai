package usagefmt

import (
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestBar(t *testing.T) {
	tests := []struct {
		name     string
		fraction float64
		width    int
		want     string
	}{
		{name: "clamps below zero", fraction: -0.1, width: 4, want: "░░░░"},
		{name: "clamps above one", fraction: 1.1, width: 4, want: "████"},
		{name: "half filled", fraction: 0.5, width: 4, want: "██░░"},
		{name: "zero width", fraction: 0.5, width: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Bar(tt.fraction, tt.width); got != tt.want {
				t.Fatalf("Bar(%v, %d) = %q, want %q", tt.fraction, tt.width, got, tt.want)
			}
		})
	}
}

func TestPercent(t *testing.T) {
	tests := []struct {
		name     string
		fraction float64
		want     int
	}{
		{name: "rounds down", fraction: 0.724, want: 72},
		{name: "rounds half up", fraction: 0.725, want: 73},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Percent(tt.fraction); got != tt.want {
				t.Fatalf("Percent(%v) = %d, want %d", tt.fraction, got, tt.want)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		name    string
		d       time.Duration
		precise bool
		want    string
	}{
		{name: "zero", d: 0, precise: true, want: "now"},
		{name: "less than one minute", d: 29 * time.Second, precise: true, want: "<1m"},
		{name: "thirty seconds rounds to a minute", d: 30 * time.Second, precise: true, want: "1m"},
		{name: "minutes precise", d: 59 * time.Minute, precise: true, want: "59m"},
		{name: "hours precise", d: 2*time.Hour + 14*time.Minute, precise: true, want: "2h14m"},
		{name: "hours rounded", d: 2*time.Hour + 14*time.Minute, precise: false, want: "2h"},
		{name: "seventy one hours precise", d: 71 * time.Hour, precise: true, want: "71h00m"},
		{name: "eighty hours uses days", d: 80 * time.Hour, precise: true, want: "3d"},
		{name: "days round at twelve hours", d: 3*24*time.Hour + 12*time.Hour, precise: false, want: "4d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Duration(tt.d, tt.precise); got != tt.want {
				t.Fatalf("Duration(%v, %v) = %q, want %q", tt.d, tt.precise, got, tt.want)
			}
		})
	}
}

func TestResetsIn(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(2*time.Hour + 14*time.Minute)
	tests := []struct {
		name    string
		reset   *time.Time
		precise bool
		want    string
	}{
		{name: "nil", reset: nil, precise: true, want: "-"},
		{name: "past", reset: &past, precise: true, want: "now"},
		{name: "future", reset: &future, precise: true, want: "2h14m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResetsIn(now, tt.reset, tt.precise); got != tt.want {
				t.Fatalf("ResetsIn() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRankOf(t *testing.T) {
	tests := []struct {
		name     string
		fraction float64
		want     Rank
	}{
		{name: "good boundary", fraction: 0.75, want: RankGood},
		{name: "below good", fraction: 0.7499, want: RankFair},
		{name: "fair boundary", fraction: 0.50, want: RankFair},
		{name: "low boundary", fraction: 0.25, want: RankLow},
		{name: "below low", fraction: 0.2499, want: RankCritical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RankOf(tt.fraction); got != tt.want {
				t.Fatalf("RankOf(%v) = %v, want %v", tt.fraction, got, tt.want)
			}
		})
	}
}

func TestWindowName(t *testing.T) {
	tests := []struct {
		name     string
		limit    *agents.UsageLimit
		fallback string
		want     string
	}{
		{name: "weekly", limit: &agents.UsageLimit{Window: "weekly", Label: "Seven days"}, fallback: "Fallback", want: "Week"},
		{name: "five hour", limit: &agents.UsageLimit{Window: "5h"}, fallback: "Fallback", want: "5h"},
		{name: "label only", limit: &agents.UsageLimit{Label: "Tokens"}, fallback: "Fallback", want: "Tokens"},
		{name: "nil", limit: nil, fallback: "Fallback", want: "Fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WindowName(tt.limit, tt.fallback); got != tt.want {
				t.Fatalf("WindowName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeadline(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	future := now.Add(2*time.Hour + 14*time.Minute)
	past := now.Add(-time.Minute)
	primaryFraction := 0.724
	secondaryFraction := 0.414
	primary := &agents.UsageLimit{Window: "5h", RemainingFraction: &primaryFraction}
	secondary := &agents.UsageLimit{Window: "weekly", RemainingFraction: &secondaryFraction}
	tests := []struct {
		name     string
		headline agents.UsageHeadline
		want     string
	}{
		{
			name:     "both with reset",
			headline: agents.UsageHeadline{Primary: primary, Secondary: secondary, NextReset: &future},
			want:     "5h 72% left · Week 41% left · Reset 2h14m",
		},
		{
			name:     "primary only",
			headline: agents.UsageHeadline{Primary: primary},
			want:     "5h 72% left",
		},
		{
			name:     "none",
			headline: agents.UsageHeadline{},
			want:     "",
		},
		{
			name:     "past reset omitted",
			headline: agents.UsageHeadline{Primary: primary, NextReset: &past},
			want:     "5h 72% left",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Headline(tt.headline, now); got != tt.want {
				t.Fatalf("Headline() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompact(t *testing.T) {
	primaryFraction := 0.724
	secondaryFraction := 0.414
	tests := []struct {
		name     string
		headline agents.UsageHeadline
		want     string
	}{
		{
			name: "both",
			headline: agents.UsageHeadline{
				Primary:   &agents.UsageLimit{Window: "5h", RemainingFraction: &primaryFraction},
				Secondary: &agents.UsageLimit{Window: "weekly", RemainingFraction: &secondaryFraction},
			},
			want: "5h 72% · W 41%",
		},
		{name: "none", headline: agents.UsageHeadline{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compact(tt.headline); got != tt.want {
				t.Fatalf("Compact() = %q, want %q", got, tt.want)
			}
		})
	}
}
