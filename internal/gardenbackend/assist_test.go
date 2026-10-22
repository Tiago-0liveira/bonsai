package gardenbackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentsbackend"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

type assistRunner func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error)

func (f assistRunner) Run(ctx context.Context, req agentsapi.AssistRequest) (json.RawMessage, error) {
	return f(ctx, req)
}

const questionsReply = `{"schema":"bonsai.garden.assist.questions/v1","kind":"bug","title":"Export","understanding":"Export fails.","questions":[{"text":"Which platforms?","why":"Compatibility.","area":"constraint","suggestions":["Desktop"]},{"text":"Which formats?","why":"Scope.","area":"scope","suggestions":[]},{"text":"Which users?","why":"Scope.","area":"scope","suggestions":[]}]}`
const draftReply = `{"schema":"bonsai.garden.assist.draft/v1","title":"Restore export","type":"bug","summary":"Restore successful export.","current_behavior":[],"desired_behavior":[],"requirements":[],"acceptance_criteria":[],"constraints":[],"open_questions":[],"resolved":[],"notes":[]}`

func assistHarness(t *testing.T) (*harness, gardenapi.AssistContext) {
	t.Helper()
	h, _ := workHarness(t)
	stamp := h.b.stamp(0, acmeID)
	return h, gardenapi.AssistContext{Stamp: stamp, RequestID: "assist-1", IssueID: 20, Number: 101, Title: "Export", Type: garden.TypeBug, Description: "Export fails.", Draft: "## Summary\nExport fails.\n", Settings: gardenapi.AssistSettings{AccountID: "claude-acct", Model: "opus", Effort: "high"}}
}

func invokeAssist(ctx context.Context, b *Backend, req gardenapi.AssistContext, draft bool) error {
	if draft {
		_, err := b.AssistDraft(ctx, gardenapi.AssistDraftRequest{AssistContext: req, Feedback: "Keep it small."})
		return err
	}
	_, err := b.AssistQuestions(ctx, gardenapi.AssistQuestionsRequest{AssistContext: req, Round: 2})
	return err
}

func changeAssistAssociation(t *testing.T, h *harness, change func(*connect.Association)) {
	t.Helper()
	_, a, err := h.b.association(context.Background())
	if err != nil || a == nil {
		t.Fatalf("association: %v", err)
	}
	change(a)
	if err := h.b.putAssociation(context.Background(), *a, func(*connect.Association) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestAssistRefusalsBeforeRunner(t *testing.T) {
	for _, draft := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			change func(*testing.T, *harness, *gardenapi.AssistContext)
			want   gardenapi.Code
		}{
			{"UTF8", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) { r.Description = string([]byte{0xff}) }, gardenapi.CodeInvalidInput},
			{"description cap", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) {
				r.Description = strings.Repeat("x", garden.MaxDescriptionBytes+1)
			}, gardenapi.CodeInvalidInput},
			{"draft cap", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) {
				r.Draft = strings.Repeat("x", edit.MaxSpecBytes+1)
			}, gardenapi.CodeInvalidInput},
			{"answer cap", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) {
				r.Answers = []assist.Answer{{QuestionID: "r1q1", State: assist.Answered, Text: strings.Repeat("x", assist.MaxAnswerBytes+1)}}
			}, gardenapi.CodeInvalidInput},
			{"request ID", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) { r.RequestID = "" }, gardenapi.CodeInvalidInput},
			{"read only", func(t *testing.T, h *harness, _ *gardenapi.AssistContext) {
				changeAssistAssociation(t, h, func(a *connect.Association) { a.Access = connect.AccessRead })
			}, gardenapi.CodeReadOnly},
			{"no access", func(t *testing.T, h *harness, _ *gardenapi.AssistContext) {
				changeAssistAssociation(t, h, func(a *connect.Association) { a.Access = connect.AccessNone })
			}, gardenapi.CodeNotConnected},
			{"disconnected", func(t *testing.T, h *harness, _ *gardenapi.AssistContext) {
				if err := h.b.removeAssociation(context.Background(), func(*connect.Association) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}, gardenapi.CodeNotConnected},
			{"skipped", func(t *testing.T, h *harness, _ *gardenapi.AssistContext) {
				changeAssistAssociation(t, h, func(a *connect.Association) { a.Mode = connect.Skip })
			}, gardenapi.CodeNotConnected},
			{"clone", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) { r.LocalRepoID = "another-clone" }, gardenapi.CodeIdentityChanged},
			{"garden", func(_ *testing.T, _ *harness, r *gardenapi.AssistContext) { r.GardenID++ }, gardenapi.CodeIdentityChanged},
			{"epoch", func(_ *testing.T, h *harness, _ *gardenapi.AssistContext) {
				h.b.reassociate(garden.GardenID{Repo: acmeID})
			}, gardenapi.CodeIdentityChanged},
			{"unavailable", func(_ *testing.T, h *harness, _ *gardenapi.AssistContext) { h.b.deps.Assist = nil }, gardenapi.CodeAssistUnsupported},
			{"input before connection", func(t *testing.T, h *harness, r *gardenapi.AssistContext) {
				r.Draft = string([]byte{0xff})
				changeAssistAssociation(t, h, func(a *connect.Association) { a.Access = connect.AccessRead })
			}, gardenapi.CodeInvalidInput},
			{"access before stamp", func(t *testing.T, h *harness, r *gardenapi.AssistContext) {
				r.Epoch++
				changeAssistAssociation(t, h, func(a *connect.Association) { a.Access = connect.AccessRead })
			}, gardenapi.CodeReadOnly},
			{"stamp before unavailable", func(_ *testing.T, h *harness, r *gardenapi.AssistContext) { r.Epoch++; h.b.deps.Assist = nil }, gardenapi.CodeIdentityChanged},
		} {
			t.Run(fmt.Sprintf("draft=%v/%s", draft, tc.name), func(t *testing.T) {
				h, req := assistHarness(t)
				var calls atomic.Int32
				h.b.deps.Assist = assistRunner(func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error) { calls.Add(1); return nil, nil })
				tc.change(t, h, &req)
				mustErrCode(t, invokeAssist(context.Background(), h.b, req, draft), tc.want)
				if calls.Load() != 0 {
					t.Fatal("refusal called runner")
				}
			})
		}
	}
}

func TestAssistQuestionLimitsBeforeRunner(t *testing.T) {
	for _, tc := range []struct {
		round, max int
		answers    int
	}{{0, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {1, assist.MaxQuestions + 1, 0}, {1, 1, assist.MaxSessionQuestions}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			h, req := assistHarness(t)
			req.Settings.Questions = tc.max
			for i := 0; i < tc.answers; i++ {
				req.Answers = append(req.Answers, assist.Answer{QuestionID: fmt.Sprintf("r1q%d", i), State: assist.Skipped})
			}
			h.b.deps.Assist = assistRunner(func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error) {
				t.Fatal("invalid question limit ran assistant")
				return nil, nil
			})
			_, err := h.b.AssistQuestions(context.Background(), gardenapi.AssistQuestionsRequest{AssistContext: req, Round: tc.round})
			mustErrCode(t, err, gardenapi.CodeInvalidInput)
		})
	}
}

func TestAssistErrorMapping(t *testing.T) {
	for _, draft := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			err  error
			want gardenapi.Code
		}{
			{"unsupported", agents.ErrStructuredUnsupported, gardenapi.CodeAssistUnsupported},
			{"timeout", context.DeadlineExceeded, gardenapi.CodeAssistTimeout},
			{"structured output", agents.ErrStructuredOutput, gardenapi.CodeAssistMalformed},
			{"malformed", assist.ErrMalformed, gardenapi.CodeAssistMalformed},
			{"large", assist.ErrTooLarge, gardenapi.CodeAssistTooLarge},
			{"large and malformed", errors.Join(assist.ErrTooLarge, assist.ErrMalformed), gardenapi.CodeAssistTooLarge},
			{"auth", agents.ErrNotAuthenticated, gardenapi.CodeAgentAuthRequired},
			{"launch", agents.ErrInvalidLaunch, gardenapi.CodeInvalidInput},
			{"account", agentsbackend.ErrAccountUnavailable, gardenapi.CodeFailed},
			{"unknown", errors.New("unknown provider failure"), gardenapi.CodeFailed},
			{"cancel", context.Canceled, ""},
		} {
			t.Run(fmt.Sprintf("draft=%v/%s", draft, tc.name), func(t *testing.T) {
				h, req := assistHarness(t)
				h.b.deps.Assist = assistRunner(func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error) {
					return []byte("RAW_PROVIDER_SECRET"), fmt.Errorf("RAW_PROVIDER_SECRET: %w", tc.err)
				})
				err := invokeAssist(context.Background(), h.b, req, draft)
				if tc.err == context.Canceled {
					if err != context.Canceled {
						t.Fatalf("cancel wrapped: %v", err)
					}
					return
				}
				ae := mustErrCode(t, err, tc.want)
				if strings.Contains(ae.Message+ae.Next+ae.Fix, "RAW_PROVIDER_SECRET") {
					t.Fatal("provider output leaked")
				}
				if tc.want == gardenapi.CodeAssistTimeout && (ae.Message != "The assistant did not answer in time. Your answers are kept." || ae.Next != "Retry, or choose a faster model.") {
					t.Fatalf("timeout copy: %+v", ae)
				}
				if tc.want == gardenapi.CodeAgentAuthRequired && ae.Next != "Sign in again in Accounts." {
					t.Fatalf("auth next: %s", ae.Next)
				}
			})
		}
	}
}

// Embedded nil ports deliberately panic if assistance touches any forbidden seam.
type forbiddenAssistGarden struct {
	garden.IssueReader
	garden.IssueWriter
	garden.IssueCreator
	garden.MetadataWriter
}
type forbiddenAssistPlanner struct {
	plan.Agents
	plan.Inspector
}

func TestAssistCallsAndDecodingHaveNoSideEffects(t *testing.T) {
	for _, draft := range []bool{false, true} {
		t.Run(fmt.Sprint(draft), func(t *testing.T) {
			h, req := assistHarness(t)
			forbidden := new(forbiddenAssistGarden)
			h.b.deps.Issues, h.b.deps.Writer, h.b.deps.Creator = forbidden, forbidden, forbidden
			planner := new(forbiddenAssistPlanner)
			h.b.deps.Agents, h.b.deps.Checkout = planner, planner
			before := h.calls()
			req.Settings.Questions = 1
			req.Settings.Guidance = "Keep scope small."
			req.Answers = []assist.Answer{{QuestionID: "r1q1", Question: "Which platforms?", State: assist.Answered, Text: "Desktop"}}
			calls := 0
			h.b.deps.Assist = assistRunner(func(ctx context.Context, got agentsapi.AssistRequest) (json.RawMessage, error) {
				calls++
				deadline, ok := ctx.Deadline()
				limit := assist.QuestionsTimeout
				if draft {
					limit = assist.DraftTimeout
				}
				if !ok || time.Until(deadline) > limit || time.Until(deadline) <= 0 {
					t.Fatal("missing call timeout")
				}
				in := assist.Input{Repository: "acme/my-app", Number: req.Number, Title: req.Title, Type: req.Type, Description: req.Description, Draft: req.Draft, Answers: req.Answers, Guidance: req.Settings.Guidance, MaxQuestions: 1}
				var call assist.Call
				var err error
				if draft {
					in.Feedback = "Keep it small."
					call, err = assist.DraftCall(in)
				} else {
					call, err = assist.QuestionsCall(in)
				}
				if err != nil {
					t.Fatal(err)
				}
				if got.AccountID != req.Settings.AccountID || got.Model != req.Settings.Model || got.Effort != req.Settings.Effort || got.System != call.System || string(got.Input) != string(call.Input) || string(got.Schema) != string(call.Schema) || got.MaxOutput != call.MaxOutput {
					t.Fatalf("runner request mismatch: %+v", got)
				}
				if draft {
					return []byte(draftReply), nil
				}
				return []byte(questionsReply), nil
			})
			if draft {
				v, err := h.b.AssistDraft(context.Background(), gardenapi.AssistDraftRequest{AssistContext: req, Feedback: "Keep it small."})
				if err != nil || v.InputHash != garden.ContentHash(req.Draft) || v.RequestID != req.RequestID || v.Proposal.Title != "Restore export" || v.Stamp.Epoch != req.Epoch || v.Generation == 0 {
					t.Fatalf("draft: %+v %v", v, err)
				}
			} else {
				v, err := h.b.AssistQuestions(context.Background(), gardenapi.AssistQuestionsRequest{AssistContext: req, Round: 3})
				if err != nil || v.RequestID != req.RequestID || v.Round != 3 || v.Model != req.Settings.Model || len(v.Set.Questions) != 1 || v.Set.Dropped != 2 || v.Set.Questions[0].ID != "r3q1" || v.Set.Questions[0].Text != "Which formats?" || v.Stamp.Epoch != req.Epoch || v.Generation == 0 {
					t.Fatalf("questions: %+v %v", v, err)
				}
			}
			if calls != 1 || h.calls() != before || h.b.captures != nil || h.b.edits != nil || h.b.approvals != nil || h.b.planner != nil || h.b.reviews != nil {
				t.Fatal("assistance touched a forbidden service or GitHub")
			}
		})
	}
}

func TestAssistDuplicateAndRelease(t *testing.T) {
	for _, draft := range []bool{false, true} {
		for _, outcome := range []string{"success", "error", "cancel", "malformed"} {
			t.Run(fmt.Sprintf("draft=%v/%s", draft, outcome), func(t *testing.T) {
				h, req := assistHarness(t)
				entered, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				h.b.deps.Assist = assistRunner(func(ctx context.Context, _ agentsapi.AssistRequest) (json.RawMessage, error) {
					if calls.Add(1) == 1 {
						close(entered)
						if outcome == "cancel" {
							<-ctx.Done()
							return nil, ctx.Err()
						}
						<-release
						if outcome == "error" {
							return nil, errors.New("failure")
						}
						if outcome == "malformed" {
							return []byte(`{"RAW_PROVIDER_SECRET":true}`), nil
						}
					}
					if draft {
						return []byte(draftReply), nil
					}
					return []byte(questionsReply), nil
				})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- invokeAssist(ctx, h.b, req, draft) }()
				<-entered
				var wg sync.WaitGroup
				for i := 0; i < 12; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						err := invokeAssist(context.Background(), h.b, req, !draft)
						if code(err) != gardenapi.CodeAssistBusy {
							t.Errorf("duplicate: %v", err)
						}
					}()
				}
				wg.Wait()
				if calls.Load() != 1 {
					t.Fatal("duplicate reached runner")
				}
				if outcome == "cancel" {
					cancel()
				} else {
					close(release)
				}
				err := <-done
				switch outcome {
				case "success":
					if err != nil {
						t.Fatal(err)
					}
				case "cancel":
					if err != context.Canceled {
						t.Fatalf("cancel: %v", err)
					}
				case "error":
					mustErrCode(t, err, gardenapi.CodeFailed)
				case "malformed":
					ae := mustErrCode(t, err, gardenapi.CodeAssistMalformed)
					if strings.Contains(ae.Message, "RAW_PROVIDER_SECRET") {
						t.Fatal("raw decode output leaked")
					}
				}
				if err := invokeAssist(context.Background(), h.b, req, draft); err != nil {
					t.Fatalf("ID not released: %v", err)
				}
				if calls.Load() != 2 {
					t.Fatalf("calls: %d", calls.Load())
				}
				h.b.mu.Lock()
				remaining := len(h.b.assistFlights)
				h.b.mu.Unlock()
				if remaining != 0 {
					t.Fatal("retained flight")
				}
			})
		}
	}
}

func TestAssistLateReplyRetainsOriginalEpoch(t *testing.T) {
	h, req := assistHarness(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.b.deps.Assist = assistRunner(func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error) {
		close(entered)
		<-release
		return []byte(draftReply), nil
	})
	done := make(chan gardenapi.AssistDraftView, 1)
	go func() {
		v, err := h.b.AssistDraft(context.Background(), gardenapi.AssistDraftRequest{AssistContext: req})
		if err != nil {
			t.Error(err)
		}
		done <- v
	}()
	<-entered
	h.b.reassociate(garden.GardenID{Repo: acmeID})
	close(release)
	v := <-done
	if v.Epoch != req.Epoch || v.Epoch == h.b.stamp(0, acmeID).Epoch {
		t.Fatalf("late reply relabeled: %+v", v.Stamp)
	}
}

func TestAssistDraftRenderedLimit(t *testing.T) {
	h, req := assistHarness(t)
	req.Draft = "## Details\n" + strings.Repeat("a", edit.MaxSpecBytes-len("## Details\n"))
	h.b.deps.Assist = assistRunner(func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error) {
		return []byte(draftReply), nil
	})
	mustErrCode(t, invokeAssist(context.Background(), h.b, req, true), gardenapi.CodeAssistTooLarge)
}

func TestAssistExpiredContextBeforeRunner(t *testing.T) {
	for _, draft := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("draft=%v/canceled=%v", draft, canceled), func(t *testing.T) {
				h, req := assistHarness(t)
				h.b.deps.Assist = assistRunner(func(context.Context, agentsapi.AssistRequest) (json.RawMessage, error) {
					t.Fatal("expired context reached runner")
					return nil, nil
				})
				var ctx context.Context
				var cancel context.CancelFunc
				if canceled {
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
				} else {
					ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
				}
				defer cancel()
				err := invokeAssist(ctx, h.b, req, draft)
				if canceled {
					if err != context.Canceled {
						t.Fatalf("cancellation wrapped: %v", err)
					}
				} else {
					mustErrCode(t, err, gardenapi.CodeAssistTimeout)
				}
			})
		}
	}
}
