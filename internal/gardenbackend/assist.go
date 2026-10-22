package gardenbackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// AssistQuestions reads no GitHub, writes nothing and keeps no record.
func (b *Backend) AssistQuestions(ctx context.Context, req gardenapi.AssistQuestionsRequest) (gardenapi.AssistQuestionsView, error) {
	// Reject invalid correlation before spending a provider call on an unusable reply.
	if req.Round < 1 || len(fmt.Sprintf("r%dq%d", req.Round, assist.MaxQuestions)) > 16 {
		return gardenapi.AssistQuestionsView{}, assistError(assist.ErrInvalidInput)
	}
	raw, in, stamp, err := b.runAssist(ctx, req.AssistContext, "", assist.QuestionsCall)
	if err != nil {
		return gardenapi.AssistQuestionsView{}, err
	}
	set, err := assist.DecodeQuestions(raw, req.Round, in.MaxQuestions, in.Answers)
	if err != nil {
		return gardenapi.AssistQuestionsView{}, assistError(err)
	}
	// The Assist port returns only JSON, so it cannot identify the provider.
	return gardenapi.AssistQuestionsView{Stamp: stamp, RequestID: req.RequestID, Round: req.Round, Set: set, Model: req.Settings.Model}, nil
}

// AssistDraft reads no GitHub, writes nothing and keeps no record.
func (b *Backend) AssistDraft(ctx context.Context, req gardenapi.AssistDraftRequest) (gardenapi.AssistDraftView, error) {
	raw, in, stamp, err := b.runAssist(ctx, req.AssistContext, req.Feedback, assist.DraftCall)
	if err != nil {
		return gardenapi.AssistDraftView{}, err
	}
	proposal, err := assist.DecodeDraft(raw, in)
	if err != nil {
		return gardenapi.AssistDraftView{}, assistError(err)
	}
	return gardenapi.AssistDraftView{Stamp: stamp, RequestID: req.RequestID, InputHash: garden.ContentHash(req.Draft), Proposal: proposal}, nil
}

func (b *Backend) runAssist(ctx context.Context, req gardenapi.AssistContext, feedback string, build func(assist.Input) (assist.Call, error)) (json.RawMessage, assist.Input, gardenapi.Stamp, error) {
	in := assist.Input{
		Number: req.Number, Title: req.Title, Type: req.Type, Description: req.Description,
		Draft: req.Draft, Answers: req.Answers, Feedback: feedback, Guidance: req.Settings.Guidance,
		MaxQuestions: req.Settings.Questions,
	}
	if in.MaxQuestions == 0 {
		in.MaxQuestions = assist.DefaultQuestions
	}
	// The first build validates caps and UTF-8 before even reading the association.
	if _, err := build(in); err != nil {
		return nil, in, gardenapi.Stamp{}, assistError(err)
	}
	if req.RequestID == "" || !utf8.ValidString(req.RequestID) {
		return nil, in, gardenapi.Stamp{}, assistError(assist.ErrInvalidInput)
	}
	_, a, err := b.association(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, in, gardenapi.Stamp{}, assistError(err)
		}
		return nil, in, gardenapi.Stamp{}, apiError(err)
	}
	// Assistance uses the same recorded target as Status's offline projection.
	// Calling writeTarget here would perform fresh GitHub reads.
	if a == nil || a.Mode == connect.Skip || a.Access == connect.AccessNone {
		return nil, in, gardenapi.Stamp{}, &gardenapi.Error{Code: gardenapi.CodeNotConnected, Message: "Garden is not set up for this clone."}
	}
	if err := requireWriteAccess(a.Access, a.RemoteFullName); err != nil {
		return nil, in, gardenapi.Stamp{}, err
	}
	stamp := b.stamp(0, a.GardenID.Repo)
	if req.LocalRepoID != stamp.LocalRepoID || req.GardenID != stamp.GardenID || req.Epoch != stamp.Epoch {
		return nil, in, gardenapi.Stamp{}, apiError(ErrRemoteChanged)
	}
	if b.deps.Assist == nil {
		return nil, in, gardenapi.Stamp{}, assistError(agents.ErrStructuredUnsupported)
	}
	b.mu.Lock()
	if _, busy := b.assistFlights[req.RequestID]; busy {
		b.mu.Unlock()
		return nil, in, gardenapi.Stamp{}, &gardenapi.Error{Code: gardenapi.CodeAssistBusy, Message: "Another AI request is running."}
	}
	if b.assistFlights == nil {
		b.assistFlights = make(map[string]struct{})
	}
	b.assistFlights[req.RequestID] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.assistFlights, req.RequestID)
		b.mu.Unlock()
	}()

	in.Repository = a.RemoteFullName
	call, err := build(in)
	if err != nil {
		return nil, in, gardenapi.Stamp{}, assistError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, call.Timeout)
	defer cancel()
	raw, err := b.deps.Assist.Run(ctx, agentsapi.AssistRequest{
		AccountID: req.Settings.AccountID, Model: req.Settings.Model, Effort: req.Settings.Effort,
		System: call.System, Input: call.Input, Schema: call.Schema, MaxOutput: call.MaxOutput,
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, in, gardenapi.Stamp{}, assistError(err)
	}
	// Keep the epoch captured before inference: a late reply must never acquire
	// the epoch of an association that changed while the provider was running.
	stamp.Generation = b.gen.Add(1)
	return raw, in, stamp, nil
}

func assistError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, agents.ErrStructuredUnsupported):
		return &gardenapi.Error{Code: gardenapi.CodeAssistUnsupported, Message: "Tool-free AI assistance is unavailable for this account.", Next: "Choose an account that supports structured assistance."}
	case errors.Is(err, context.DeadlineExceeded):
		return &gardenapi.Error{Code: gardenapi.CodeAssistTimeout, Message: "The assistant did not answer in time. Your answers are kept.", Next: "Retry, or choose a faster model."}
	case errors.Is(err, assist.ErrTooLarge):
		return &gardenapi.Error{Code: gardenapi.CodeAssistTooLarge, Message: "The proposed draft is too large. Your answers are kept.", Next: "Ask for a smaller draft."}
	case errors.Is(err, agents.ErrStructuredOutput), errors.Is(err, assist.ErrMalformed):
		return &gardenapi.Error{Code: gardenapi.CodeAssistMalformed, Message: "The assistant returned an invalid reply. Your answers are kept.", Next: "Retry the request."}
	case errors.Is(err, agents.ErrNotAuthenticated):
		return &gardenapi.Error{Code: gardenapi.CodeAgentAuthRequired, Message: "The agent account needs sign-in.", Next: "Sign in again in Accounts."}
	case errors.Is(err, agents.ErrInvalidLaunch), errors.Is(err, assist.ErrInvalidInput):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The AI request or model settings are invalid."}
	default:
		return &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "The assistant could not complete the request. Your answers are kept.", Next: "Retry the request."}
	}
}
