package bitrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/portfolio/auditor-ia/internal/audit/domain"
)

type Source struct {
	Webhook string
	Client  *http.Client
}

func (s Source) Records(
	ctx context.Context,
) ([]domain.Record, error) {
	if s.Webhook == "" {
		return demoRecords(), nil
	}

	client := s.Client
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	var records []domain.Record
	start := 0

	for {
		endpoint := fmt.Sprintf(
			"%scrm.deal.list.json?start=%d",
			s.Webhook,
			start,
		)

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			endpoint,
			nil,
		)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf(
				"Bitrix crm.deal.list: HTTP %d",
				resp.StatusCode,
			)
		}

		var payload struct {
			Result []map[string]any `json:"result"`
			Next   *int             `json:"next"`
		}

		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		for _, item := range payload.Result {
			records = append(
				records,
				normalize("deal", item),
			)
		}

		if payload.Next == nil {
			break
		}

		start = *payload.Next
	}

	return records, nil
}

func normalize(entity string, raw map[string]any) domain.Record {
	return domain.Record{Type: entity, ID: fmt.Sprint(raw["ID"]), Fields: map[string]any{"owner_id": raw["ASSIGNED_BY_ID"], "updated_at": raw["DATE_MODIFY"], "amount": raw["OPPORTUNITY"], "closed": raw["CLOSED"]}}
}

func demoRecords() []domain.Record {
	return []domain.Record{
		{Type: "deal", ID: "D-202", Fields: map[string]any{"owner_id": "7", "amount": 0}},
	}
}
