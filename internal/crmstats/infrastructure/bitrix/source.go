package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	dealbitrix "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	platformbitrix "github.com/portfolio/auditor-ia/internal/platform/bitrix"
)

type Source struct {
	Webhook    string
	Client     *http.Client
	PageDelay  time.Duration
	MaxRetries int
}

func (s Source) CollectDeals(ctx context.Context, progress func(statsdomain.Progress)) ([]dealdomain.Deal, error) {
	seen := make(map[int64]dealdomain.Deal)
	start := 0
	for {
		var response struct {
			Result []json.RawMessage `json:"result"`
			Next   json.RawMessage   `json:"next"`
		}
		payload := map[string]any{
			"order":  map[string]string{"ID": "ASC"},
			"select": []string{"ID", "TITLE", "ASSIGNED_BY_ID", "DATE_CREATE", "DATE_MODIFY", "STAGE_ID", "STAGE_SEMANTIC_ID", "CLOSED", "OPPORTUNITY", "CURRENCY_ID"},
			"start":  start,
		}
		if err := s.callWithRetry(ctx, "crm.deal.list.json", payload, &response); err != nil {
			return nil, fmt.Errorf("listar negócios Bitrix: %w", err)
		}
		for _, raw := range response.Result {
			deal, err := dealbitrix.MapDeal(raw)
			if err != nil {
				return nil, fmt.Errorf("mapear negócio da listagem: %w", err)
			}
			seen[deal.BitrixDealID] = deal
		}
		if progress != nil {
			progress(statsdomain.Progress{Phase: "deals", DealsCollected: len(seen)})
		}
		next, ok, err := nextPage(response.Next)
		if err != nil {
			return nil, fmt.Errorf("paginação de negócios inválida: %w", err)
		}
		if !ok {
			break
		}
		start = next
		if err := s.waitPage(ctx); err != nil {
			return nil, err
		}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	deals := make([]dealdomain.Deal, 0, len(ids))
	for _, id := range ids {
		deals = append(deals, seen[id])
	}
	return deals, nil
}

func (s Source) CollectMarker3264(ctx context.Context, progress func(statsdomain.Progress)) ([]int64, int, error) {
	owners := make(map[int64]struct{})
	start, activities := 0, 0
	for {
		var response struct {
			Result []struct {
				ID         json.RawMessage `json:"ID"`
				OwnerID    json.RawMessage `json:"OWNER_ID"`
				Subject    string          `json:"SUBJECT"`
				ProviderID string          `json:"PROVIDER_ID"`
			} `json:"result"`
			Next json.RawMessage `json:"next"`
		}
		payload := map[string]any{
			"order":  map[string]string{"ID": "ASC"},
			"filter": map[string]any{"OWNER_TYPE_ID": 2, "PROVIDER_ID": "IMOPENLINES_SESSION"},
			"select": []string{"ID", "OWNER_ID", "SUBJECT", "PROVIDER_ID"},
			"start":  start,
		}
		err := s.callWithRetry(ctx, "crm.activity.list.json", payload, &response)
		if errors.Is(err, platformbitrix.ErrAccessDenied) {
			return nil, activities, statsdomain.ErrMarkerUnavailable
		}
		if err != nil {
			return nil, activities, fmt.Errorf("listar atividades IMOPENLINES: %w", err)
		}
		activities += len(response.Result)
		for _, activity := range response.Result {
			if activity.ProviderID != "" && !strings.EqualFold(activity.ProviderID, "IMOPENLINES_SESSION") {
				continue
			}
			if !containsExact3264(activity.Subject) {
				continue
			}
			owner, ok := rawPositiveInt64(activity.OwnerID)
			if ok {
				owners[owner] = struct{}{}
			}
		}
		if progress != nil {
			progress(statsdomain.Progress{Phase: "activities", ActivitiesCollected: activities, Markers3264Discovered: len(owners)})
		}
		next, ok, err := nextPage(response.Next)
		if err != nil {
			return nil, activities, fmt.Errorf("paginação de atividades inválida: %w", err)
		}
		if !ok {
			break
		}
		start = next
		if err := s.waitPage(ctx); err != nil {
			return nil, activities, err
		}
	}
	ids := make([]int64, 0, len(owners))
	for id := range owners {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, activities, nil
}

func (s Source) GetAssignee(ctx context.Context, id int64) (dealdomain.Assignee, error) {
	var result dealdomain.Assignee
	err := s.retry(ctx, func() error {
		var err error
		result, err = dealbitrix.GetAssignee(ctx, s.Webhook, s.Client, id)
		return err
	})
	return result, err
}

func (s Source) callWithRetry(ctx context.Context, method string, payload, target any) error {
	client := platformbitrix.Client{Webhook: s.Webhook, HTTP: s.Client}
	return s.retry(ctx, func() error { return client.Call(ctx, method, payload, target) })
}

func (s Source) retry(ctx context.Context, call func() error) error {
	max := s.MaxRetries
	if max <= 0 {
		max = 4
	}
	var err error
	for attempt := 0; attempt < max; attempt++ {
		if err = call(); err == nil {
			return nil
		}
		var apiErr *platformbitrix.APIError
		if !errors.As(err, &apiErr) || (apiErr.HTTPStatus != http.StatusTooManyRequests && apiErr.HTTPStatus < 500) {
			return err
		}
		delay := time.Duration(1<<attempt) * 250 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func (s Source) waitPage(ctx context.Context) error {
	if s.PageDelay <= 0 {
		return nil
	}
	timer := time.NewTimer(s.PageDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextPage(raw json.RawMessage) (int, bool, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return 0, false, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := strconv.Atoi(number.String())
		return value, err == nil && value > 0, err
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false, err
	}
	value, err := strconv.Atoi(text)
	return value, err == nil && value > 0, err
}

func rawPositiveInt64(raw json.RawMessage) (int64, bool) {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

func containsExact3264(subject string) bool {
	for index := 0; index+4 <= len(subject); index++ {
		if subject[index:index+4] != "3264" {
			continue
		}
		beforeDigit := index > 0 && subject[index-1] >= '0' && subject[index-1] <= '9'
		after := index + 4
		afterDigit := after < len(subject) && subject[after] >= '0' && subject[after] <= '9'
		if !beforeDigit && !afterDigit {
			return true
		}
	}
	return false
}

var _ statsdomain.Source = Source{}
