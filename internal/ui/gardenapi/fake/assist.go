package fake

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// AssistQuestions returns the scripted reply without changing the working draft.
func (b *Backend) AssistQuestions(ctx context.Context, req gardenapi.AssistQuestionsRequest) (gardenapi.AssistQuestionsView, error) {
	gen, epoch := b.begin("AssistQuestions")
	b.mu.Lock()
	defer b.mu.Unlock()
	req.Answers = append([]assist.Answer(nil), req.Answers...)
	b.AssistQuestionsRequests = append(b.AssistQuestionsRequests, req)
	if b.AssistQuestionsErr != nil {
		return gardenapi.AssistQuestionsView{}, b.AssistQuestionsErr
	}
	v := b.AssistQuestionsView
	v.Stamp = stamp(gen, epoch, b.StatusView.Target.RepoID)
	v.RequestID, v.Round, v.PrivateDraft = req.RequestID, req.Round, req.PrivateDraft
	if v.Model == "" {
		v.Model = req.Settings.Model
	}
	return v, ctx.Err()
}

// AssistDraft returns the scripted proposal without saving or approving it.
func (b *Backend) AssistDraft(ctx context.Context, req gardenapi.AssistDraftRequest) (gardenapi.AssistDraftView, error) {
	gen, epoch := b.begin("AssistDraft")
	b.mu.Lock()
	defer b.mu.Unlock()
	req.Answers = append([]assist.Answer(nil), req.Answers...)
	b.AssistDraftRequests = append(b.AssistDraftRequests, req)
	if b.AssistDraftErr != nil {
		return gardenapi.AssistDraftView{}, b.AssistDraftErr
	}
	v := b.AssistDraftView
	v.Stamp = stamp(gen, epoch, b.StatusView.Target.RepoID)
	v.RequestID, v.PrivateDraft = req.RequestID, req.PrivateDraft
	v.InputHash = garden.ContentHash(req.Draft)
	return v, ctx.Err()
}
