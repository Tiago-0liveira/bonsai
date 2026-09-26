package antigravity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func ParseUsage(data []byte) ([]agents.UsageLimit, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse antigravity usage: %w", err)
	}
	groups, found := findGroups(root)
	if !found {
		return nil, fmt.Errorf("parse antigravity usage: quota groups not found")
	}
	var limits []agents.UsageLimit
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		groupName := firstString(group, "displayName", "display_name", "label", "name", "group", "title")
		buckets, _ := firstSlice(group, "buckets", "limits", "quotas")
		for _, rawBucket := range buckets {
			bucket, ok := rawBucket.(map[string]any)
			if !ok {
				continue
			}
			id := firstString(bucket, "bucketId", "bucket_id", "id", "quota_id", "quotaId", "name")
			window := firstString(bucket, "window", "period")
			if window == "" {
				switch {
				case strings.HasSuffix(strings.ToLower(id), "weekly"):
					window = "weekly"
				case strings.HasSuffix(strings.ToLower(id), "5h"):
					window = "5h"
				}
			}
			label := firstString(bucket, "displayName", "display_name", "label", "name", "title")
			if label == "" {
				label = id
			}
			remaining := fractionValue(bucket)
			reset := resetValue(bucket)
			limits = append(limits, agents.UsageLimit{
				ID: id, Group: groupName, Label: label, Window: window,
				RemainingFraction: remaining, ResetsAt: reset,
			})
		}
	}
	return limits, nil
}

func findGroups(v any) ([]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		if groups, ok := firstSlice(x, "groups", "quota_groups", "quotaGroups"); ok {
			return groups, true
		}
		for _, key := range []string{"data", "command", "result", "response", "usage"} {
			if child, ok := x[key]; ok {
				if groups, found := findGroups(child); found {
					return groups, true
				}
			}
		}
		for _, child := range x {
			if groups, found := findGroups(child); found {
				return groups, true
			}
		}
	case []any:
		for _, child := range x {
			if groups, found := findGroups(child); found {
				return groups, true
			}
		}
	}
	return nil, false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		switch v := m[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case json.Number:
			return v.String()
		}
	}
	return ""
}

func firstSlice(m map[string]any, keys ...string) ([]any, bool) {
	for _, key := range keys {
		if v, ok := m[key].([]any); ok {
			return v, true
		}
	}
	return nil, false
}

func fractionValue(m map[string]any) *float64 {
	for _, key := range []string{"remaining_fraction", "remainingFraction", "fraction_remaining", "fractionRemaining"} {
		if f, ok := numberValue(m[key]); ok {
			f = math.Max(0, math.Min(1, f))
			return &f
		}
	}
	if remaining, ok := m["remaining"].(map[string]any); ok {
		for _, key := range []string{"fraction", "ratio", "remaining_fraction", "remainingFraction"} {
			if f, ok := numberValue(remaining[key]); ok {
				f = math.Max(0, math.Min(1, f))
				return &f
			}
		}
	}
	return nil
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func resetValue(m map[string]any) *time.Time {
	for _, key := range []string{"reset_time", "resetTime", "resets_at", "resetsAt", "reset"} {
		switch v := m[key].(type) {
		case string:
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				t = t.UTC()
				return &t
			}
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				t := numericTime(n)
				if !t.IsZero() {
					return &t
				}
			}
		case json.Number:
			if n, err := v.Int64(); err == nil {
				t := numericTime(n)
				if !t.IsZero() {
					return &t
				}
			}
		case float64:
			t := numericTime(int64(v))
			if !t.IsZero() {
				return &t
			}
		}
	}
	return nil
}

func numericTime(n int64) time.Time {
	if n > 10_000_000_000 {
		n /= 1000
	}
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}
