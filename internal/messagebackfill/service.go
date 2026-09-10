package messagebackfill

import (
	"context"
	"strconv"
	"sync"
)

import conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"

type Deal struct {
	InternalID int64
	BitrixID   int64
}

type Selection struct {
	DealID              int64
	Limit               int
	IncludeWithMessages bool
}

type DealRepository interface {
	Select(context.Context, Selection) ([]Deal, error)
}

type Summary struct {
	TotalSelected, Processed, DealsWithMessages, MessagesCollected, MessagesPersisted int64
	NoConversation, EmptyConversation, AccessDenied, Failed                           int64
	SelectedDealIDs                                                                   []int64
}

type Service struct {
	Deals       DealRepository
	Source      conversationdomain.ConversationSource
	Messages    conversationdomain.MessageRepository
	Concurrency int
}

func (s Service) Run(ctx context.Context, selection Selection, dryRun bool, progress func(Summary)) (Summary, error) {
	deals, err := s.Deals.Select(ctx, selection)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{TotalSelected: int64(len(deals))}
	for _, deal := range deals {
		summary.SelectedDealIDs = append(summary.SelectedDealIDs, deal.BitrixID)
	}
	if dryRun || len(deals) == 0 {
		if progress != nil {
			progress(summary)
		}
		return summary, nil
	}
	concurrency := s.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan Deal)
	var group sync.WaitGroup
	var mu sync.Mutex
	update := func(apply func(*Summary)) {
		mu.Lock()
		apply(&summary)
		snapshot := summary
		mu.Unlock()
		if progress != nil {
			progress(snapshot)
		}
	}
	for range concurrency {
		group.Add(1)
		go func() {
			defer group.Done()
			for deal := range jobs {
				conversation, collectErr := s.Source.Conversation(ctx, strconv.FormatInt(deal.BitrixID, 10))
				if collectErr != nil {
					update(func(v *Summary) { v.Processed++; v.Failed++ })
					continue
				}
				collected := len(conversation.Messages)
				persisted := 0
				if collected > 0 {
					result, persistErr := s.Messages.UpsertBatch(ctx, deal.InternalID, conversation.Messages)
					if persistErr != nil {
						update(func(v *Summary) { v.Processed++; v.MessagesCollected += int64(collected); v.Failed++ })
						continue
					}
					persisted = result.Processed
				}
				update(func(v *Summary) {
					v.Processed++
					v.MessagesCollected += int64(collected)
					v.MessagesPersisted += int64(persisted)
					if collected > 0 {
						v.DealsWithMessages++
					}
					switch conversation.Status {
					case conversationdomain.ConversationAccessDenied:
						v.AccessDenied++
					case conversationdomain.ConversationEmpty:
						v.EmptyConversation++
					case conversationdomain.ConversationNoConversation:
						v.NoConversation++
					}
				})
			}
		}()
	}
send:
	for _, deal := range deals {
		select {
		case jobs <- deal:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	group.Wait()
	if ctx.Err() != nil {
		return summary, ctx.Err()
	}
	return summary, nil
}
