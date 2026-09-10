package messagebackfill

import (
	"context"
	"errors"
	"testing"

	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type dealRepoStub struct {
	deals     []Deal
	selection Selection
}

func (s *dealRepoStub) Select(_ context.Context, selection Selection) ([]Deal, error) {
	s.selection = selection
	return s.deals, nil
}

type sourceStub struct {
	byID map[string]conversationdomain.Conversation
	err  map[string]error
}

func (s sourceStub) Conversation(_ context.Context, id string) (conversationdomain.Conversation, error) {
	return s.byID[id], s.err[id]
}

type messageRepoStub struct{ calls, messages int }

func (s *messageRepoStub) UpsertBatch(_ context.Context, _ int64, messages []conversationdomain.Message) (conversationdomain.MessageUpsertResult, error) {
	s.calls++
	s.messages += len(messages)
	return conversationdomain.MessageUpsertResult{Processed: len(messages)}, nil
}

func TestBackfillDryRunDoesNotCollectOrPersist(t *testing.T) {
	deals := &dealRepoStub{deals: []Deal{{InternalID: 1, BitrixID: 52}}}
	messages := &messageRepoStub{}
	summary, err := (Service{Deals: deals, Source: sourceStub{}, Messages: messages}).Run(context.Background(), Selection{IncludeWithMessages: true, Limit: 10}, true, nil)
	if err != nil || summary.TotalSelected != 1 || summary.Processed != 0 || messages.calls != 0 || !deals.selection.IncludeWithMessages {
		t.Fatalf("dry-run incorreto: summary=%#v calls=%d err=%v", summary, messages.calls, err)
	}
}

func TestBackfillContinuesAfterIndividualErrorAndPersistsMultipleSessions(t *testing.T) {
	deals := &dealRepoStub{deals: []Deal{{InternalID: 1, BitrixID: 10}, {InternalID: 2, BitrixID: 20}}}
	messages := &messageRepoStub{}
	source := sourceStub{byID: map[string]conversationdomain.Conversation{"20": {Status: conversationdomain.ConversationAvailable, Messages: []conversationdomain.Message{{ID: "1", SessionID: "A"}, {ID: "2", SessionID: "B"}}}}, err: map[string]error{"10": errors.New("falha")}}
	summary, err := (Service{Deals: deals, Source: source, Messages: messages, Concurrency: 2}).Run(context.Background(), Selection{Limit: 10}, false, nil)
	if err != nil || summary.Processed != 2 || summary.Failed != 1 || summary.DealsWithMessages != 1 || summary.MessagesCollected != 2 || summary.MessagesPersisted != 2 || messages.calls != 1 {
		t.Fatalf("backfill incorreto: summary=%#v calls=%d err=%v", summary, messages.calls, err)
	}
}
