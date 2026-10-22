package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func TestAssistScriptsAndRecording(t *testing.T) {
	for _, draft := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			name := "questions"
			if draft {
				name = "draft"
			}
			if fail {
				name += "/error"
			}
			t.Run(name, func(t *testing.T) {
				b := New(gardenapi.StateConnected)
				req := gardenapi.AssistContext{RequestID: "session-1-seq-2", Draft: "## Summary\nExport", Answers: []assist.Answer{{QuestionID: "r1q1", State: assist.Answered, Text: "Desktop"}}, Settings: gardenapi.AssistSettings{Model: "opus"}}
				boom := errors.New("scripted failure")
				if fail {
					b.AssistQuestionsErr, b.AssistDraftErr = boom, boom
				}
				b.AssistQuestionsView = gardenapi.AssistQuestionsView{Provider: "claude", Set: assist.QuestionSet{Title: "Questions", Questions: []assist.Question{{ID: "r2q1", Text: "Which formats?"}}}}
				b.AssistDraftView = gardenapi.AssistDraftView{Proposal: assist.Proposal{Title: "Proposal"}}
				if draft {
					v, err := b.AssistDraft(context.Background(), gardenapi.AssistDraftRequest{AssistContext: req, Feedback: "Small"})
					if fail {
						if !errors.Is(err, boom) {
							t.Fatal(err)
						}
					} else if err != nil || v.Proposal.Title != "Proposal" || v.RequestID != req.RequestID || v.InputHash != garden.ContentHash(req.Draft) || v.Generation == 0 {
						t.Fatalf("draft: %+v %v", v, err)
					}
					if b.Count("AssistDraft") != 1 || len(b.AssistDraftRequests) != 1 || b.AssistDraftRequests[0].Feedback != "Small" {
						t.Fatal("missing draft request")
					}
				} else {
					v, err := b.AssistQuestions(context.Background(), gardenapi.AssistQuestionsRequest{AssistContext: req, Round: 2})
					if fail {
						if !errors.Is(err, boom) {
							t.Fatal(err)
						}
					} else if err != nil || v.Set.Title != "Questions" || v.Provider != "claude" || v.Model != "opus" || v.Round != 2 || v.RequestID != req.RequestID || v.Generation == 0 {
						t.Fatalf("questions: %+v %v", v, err)
					}
					if b.Count("AssistQuestions") != 1 || len(b.AssistQuestionsRequests) != 1 || b.AssistQuestionsRequests[0].Round != 2 {
						t.Fatal("missing questions request")
					}
				}
				req.Answers[0].Text = "Edited later"
				if draft {
					if b.AssistDraftRequests[0].Answers[0].Text != "Desktop" {
						t.Fatal("aliased answers")
					}
				} else {
					if b.AssistQuestionsRequests[0].Answers[0].Text != "Desktop" {
						t.Fatal("aliased answers")
					}
				}
				if b.Writes() != 0 {
					t.Fatal("assistant wrote")
				}
			})
		}
	}
}

func TestAssistHookKeepsOldEpoch(t *testing.T) {
	b := New(gardenapi.StateConnected)
	entered, release := make(chan struct{}), make(chan struct{})
	b.Hook = func(method string) {
		if method == "AssistDraft" {
			close(entered)
			<-release
		}
	}
	done := make(chan gardenapi.AssistDraftView, 1)
	go func() { v, _ := b.AssistDraft(context.Background(), gardenapi.AssistDraftRequest{}); done <- v }()
	<-entered
	fresh, err := b.Skip(context.Background(), gardenapi.TargetRef{})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	old := <-done
	if old.Epoch >= fresh.Stamp.Epoch {
		t.Fatal("late reply has new epoch")
	}
}

func TestAssistCancellation(t *testing.T) {
	b := New(gardenapi.StateConnected)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.AssistQuestions(ctx, gardenapi.AssistQuestionsRequest{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := b.AssistDraft(ctx, gardenapi.AssistDraftRequest{}); err != context.Canceled {
		t.Fatal(err)
	}
}
