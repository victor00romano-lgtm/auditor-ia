package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	platformbitrix "github.com/portfolio/auditor-ia/internal/platform/bitrix"
)

var decimalPattern = regexp.MustCompile(`^[+-]?\d+(?:\.\d+)?$`)

type Source struct {
	Webhook string
	Client  *http.Client
	Now     func() time.Time
}

func (s Source) Get(ctx context.Context, dealID string) (dealdomain.Deal, error) {
	if strings.TrimSpace(s.Webhook) == "" {
		return dealdomain.Deal{}, fmt.Errorf("buscar negócio no Bitrix: BITRIX_WEBHOOK_URL não configurado")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(dealID), 10, 64)
	if err != nil || id <= 0 {
		return dealdomain.Deal{}, fmt.Errorf("buscar negócio no Bitrix: ID inválido %q", dealID)
	}

	var response struct {
		Result json.RawMessage `json:"result"`
	}
	client := platformbitrix.Client{Webhook: s.Webhook, HTTP: s.Client}
	if err := client.Call(ctx, "crm.deal.get.json", map[string]any{"ID": id}, &response); err != nil {
		return dealdomain.Deal{}, fmt.Errorf("buscar negócio %d no Bitrix: %w", id, err)
	}
	result := bytes.TrimSpace(response.Result)
	if len(result) == 0 || bytes.Equal(result, []byte("null")) || bytes.Equal(result, []byte("false")) {
		return dealdomain.Deal{}, fmt.Errorf("negócio Bitrix %d não encontrado", id)
	}

	deal, err := mapDeal(result)
	if err != nil {
		return dealdomain.Deal{}, fmt.Errorf("mapear negócio Bitrix %d: %w", id, err)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	syncedAt := now().UTC()
	deal.SyncedAt = &syncedAt
	return deal, nil
}

func mapDeal(raw json.RawMessage) (dealdomain.Deal, error) {
	var item struct {
		ID              json.RawMessage `json:"ID"`
		Title           json.RawMessage `json:"TITLE"`
		StageID         json.RawMessage `json:"STAGE_ID"`
		StageSemanticID json.RawMessage `json:"STAGE_SEMANTIC_ID"`
		Closed          json.RawMessage `json:"CLOSED"`
		Opportunity     json.RawMessage `json:"OPPORTUNITY"`
		Currency        json.RawMessage `json:"CURRENCY_ID"`
		AssignedByID    json.RawMessage `json:"ASSIGNED_BY_ID"`
		DateCreate      json.RawMessage `json:"DATE_CREATE"`
		DateModify      json.RawMessage `json:"DATE_MODIFY"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return dealdomain.Deal{}, fmt.Errorf("resposta JSON inválida: %w", err)
	}
	id, err := requiredPositiveInt64(item.ID, "ID")
	if err != nil {
		return dealdomain.Deal{}, err
	}
	closed, err := optionalBool(item.Closed, "CLOSED")
	if err != nil {
		return dealdomain.Deal{}, err
	}
	amount, err := optionalDecimal(item.Opportunity, "OPPORTUNITY")
	if err != nil {
		return dealdomain.Deal{}, err
	}
	assignedByID, err := optionalPositiveInt64(item.AssignedByID, "ASSIGNED_BY_ID")
	if err != nil {
		return dealdomain.Deal{}, err
	}
	createdAt, err := optionalTime(item.DateCreate, "DATE_CREATE")
	if err != nil {
		return dealdomain.Deal{}, err
	}
	updatedAt, err := optionalTime(item.DateModify, "DATE_MODIFY")
	if err != nil {
		return dealdomain.Deal{}, err
	}
	return dealdomain.Deal{
		BitrixDealID:    id,
		Title:           optionalString(item.Title),
		StageID:         optionalString(item.StageID),
		StageSemanticID: optionalString(item.StageSemanticID),
		Closed:          closed,
		Amount:          amount,
		Currency:        optionalString(item.Currency),
		AssignedByID:    assignedByID,
		CreatedAtBitrix: createdAt,
		UpdatedAtBitrix: updatedAt,
	}, nil
}

// MapDeal maps the selected fields returned by crm.deal.list. It is exported
// for metadata synchronizers so they share the exact validation used by Get.
func MapDeal(raw json.RawMessage) (dealdomain.Deal, error) { return mapDeal(raw) }

func optionalString(raw json.RawMessage) *string {
	value, ok := rawScalar(raw)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func requiredPositiveInt64(raw json.RawMessage, field string) (int64, error) {
	value, ok := rawScalar(raw)
	if !ok {
		return 0, fmt.Errorf("%s ausente", field)
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s inválido: %q", field, value)
	}
	return id, nil
}

func optionalPositiveInt64(raw json.RawMessage, field string) (*int64, error) {
	value, ok := rawScalar(raw)
	if !ok || strings.TrimSpace(value) == "" || value == "0" {
		return nil, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("%s inválido: %q", field, value)
	}
	return &id, nil
}

func optionalBool(raw json.RawMessage, field string) (*bool, error) {
	value, ok := rawScalar(raw)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var result bool
	switch strings.ToUpper(value) {
	case "Y", "TRUE", "1":
		result = true
	case "N", "FALSE", "0":
		result = false
	default:
		return nil, fmt.Errorf("%s inválido: %q", field, value)
	}
	return &result, nil
}

func optionalDecimal(raw json.RawMessage, field string) (*string, error) {
	value, ok := rawScalar(raw)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if !decimalPattern.MatchString(value) {
		return nil, fmt.Errorf("%s inválido: %q", field, value)
	}
	return &value, nil
}

func optionalTime(raw json.RawMessage, field string) (*time.Time, error) {
	value, ok := rawScalar(raw)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("%s inválido: %q", field, value)
}

func rawScalar(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	var boolean bool
	if json.Unmarshal(raw, &boolean) == nil {
		return strconv.FormatBool(boolean), true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if decoder.Decode(&number) == nil {
		return number.String(), true
	}
	return "", false
}

var _ dealdomain.DealSource = Source{}
