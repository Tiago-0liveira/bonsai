package garden

import (
	"slices"
	"strings"
)

const ManagedLabel = "bonsai:managed"
const TypePrefix = "bonsai:type/"
const StagePrefix = "bonsai:stage/"

type ItemType string

const (
	TypeFeature  ItemType = "feature"
	TypeBug      ItemType = "bug"
	TypeChore    ItemType = "chore"
	TypeResearch ItemType = "research"
)

type Stage string

const (
	StageDraft      Stage = "draft"
	StageReady      Stage = "ready"
	StageInProgress Stage = "in-progress"
	StageReview     Stage = "review"
	StageBlocked    Stage = "blocked"
	StageDone       Stage = "done"
)

type LabelProblem string

const (
	ProblemMissingManaged LabelProblem = "missing-managed-marker"
	ProblemTypeCount      LabelProblem = "type-count"
	ProblemStageCount     LabelProblem = "stage-count"
	ProblemUnknown        LabelProblem = "unknown-namespaced-label"
)

type Classification struct {
	Type     ItemType
	Stage    Stage
	Problems []LabelProblem
}

// FormatTypeLabel and FormatStageLabel produce the canonical namespaced labels.
func FormatTypeLabel(v ItemType) string { return TypePrefix + string(v) }
func FormatStageLabel(v Stage) string   { return StagePrefix + string(v) }

// Classify reports malformed managed label sets without guessing a winner.
func Classify(labels []string) Classification {
	var out Classification
	var types []ItemType
	var stages []Stage
	seen := map[string]bool{}
	for _, l := range labels {
		key := l
		if strings.HasPrefix(strings.ToLower(l), "bonsai:") {
			key = strings.ToLower(l)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		namespace := strings.HasPrefix(strings.ToLower(l), "bonsai:")
		switch {
		case l == ManagedLabel:
		case strings.HasPrefix(l, TypePrefix):
			v := ItemType(strings.TrimPrefix(l, TypePrefix))
			switch v {
			case TypeFeature, TypeBug, TypeChore, TypeResearch:
				types = append(types, v)
			default:
				out.Problems = append(out.Problems, ProblemUnknown)
			}
		case strings.HasPrefix(l, StagePrefix):
			v := Stage(strings.TrimPrefix(l, StagePrefix))
			switch v {
			case StageDraft, StageReady, StageInProgress, StageReview, StageBlocked, StageDone:
				stages = append(stages, v)
			default:
				out.Problems = append(out.Problems, ProblemUnknown)
			}
		case namespace:
			out.Problems = append(out.Problems, ProblemUnknown)
		}
	}
	managed := false
	for _, l := range labels {
		if l == ManagedLabel {
			managed = true
		}
	}
	if !managed {
		out.Problems = append(out.Problems, ProblemMissingManaged)
	}
	if len(types) != 1 {
		out.Problems = append(out.Problems, ProblemTypeCount)
	} else {
		out.Type = types[0]
	}
	if len(stages) != 1 {
		out.Problems = append(out.Problems, ProblemStageCount)
	} else {
		out.Stage = stages[0]
	}
	return out
}

// ManagedLabels returns a sorted copy for comparing concurrent managed-label edits.
func ManagedLabels(labels []string) []string {
	var out []string
	for _, l := range labels {
		if strings.HasPrefix(strings.ToLower(l), "bonsai:") {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}
