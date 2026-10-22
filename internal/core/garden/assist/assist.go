// Package assist builds tool-free drafting calls and validates their proposals.
// It does not approve, persist or execute any part of a proposal.
package assist

import (
	"encoding/json"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

const (
	DefaultQuestions    = 5
	MaxQuestions        = 10
	MaxSessionQuestions = 30
	MaxAnswerBytes      = 2 << 10
	MaxGuidanceRunes    = 300
	MaxFeedbackRunes    = 1000
	MaxInputBytes       = 128 << 10
	MaxOutputBytes      = 256 << 10
	MaxPayloadBytes     = 64 << 10
	QuestionsTimeout    = 120 * time.Second
	DraftTimeout        = 240 * time.Second
)

type AnswerState string

const (
	Answered AnswerState = "answered"
	Skipped  AnswerState = "skipped"
	Unknown  AnswerState = "unknown"
	Open     AnswerState = "open"
)

type Question struct {
	ID          string
	Round       int
	Text        string
	Why         string
	Area        string
	Suggestions []string
}

type Answer struct {
	QuestionID string
	Question   string
	State      AnswerState
	Choice     string // chosen suggestion text; "" when none
	Text       string // own answer, or notes on Choice or on an unknown
}

// MaxNewOpenQuestions bounds the open questions a draft reply may add, so the
// model cannot bury the person in doubts they cannot answer inside the assistant.
const MaxNewOpenQuestions = 3

type Item struct {
	Text       string `json:"text"`
	Basis      string `json:"basis"`
	QuestionID string `json:"question_id"`
}

// Draft is spec.Render output of the working draft, or empty for a new idea.
type Input struct {
	Repository   string
	Number       int64
	Title        string
	Type         garden.ItemType
	Description  string
	Draft        string
	Answers      []Answer
	Feedback     string
	Guidance     string
	MaxQuestions int
}

// System contains only fixed instructions; Input carries all user material.
type Call struct {
	System    string
	Input     []byte
	Schema    json.RawMessage
	Timeout   time.Duration
	MaxOutput int64
}

func QuestionsCall(in Input) (Call, error) { return buildCall(in, "questions") }
func DraftCall(in Input) (Call, error)     { return buildCall(in, "draft") }

type QuestionSet struct {
	Kind          string
	Title         string
	Understanding string
	Questions     []Question
	Dropped       int
}

type Proposal struct {
	Title              string
	Type               garden.ItemType
	Summary            string
	CurrentBehavior    []Item
	DesiredBehavior    []Item
	Requirements       []Item
	AcceptanceCriteria []Item
	Constraints        []Item
	Open               []string
	Notes              []string
	Carried            int
	Dropped            int
	extra              []spec.Section
	// The editor's original list markers and checked lines must survive verbatim.
	existingLines []string
	existingCount int
}
