package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/conversation/domain"
	"github.com/portfolio/auditor-ia/internal/conversation/privacy"
	platformbitrix "github.com/portfolio/auditor-ia/internal/platform/bitrix"
)

type Timeline struct {
	Webhook string
	Client  *http.Client
}

type openLinesActivity struct {
	ID        string
	SessionID string
}

// Conversation preserves the difference between an absent conversation, an
// empty history and a history that exists but cannot be read by this webhook.
func (t Timeline) Conversation(ctx context.Context, dealID string) (domain.Conversation, error) {
	activities, err := t.openLinesActivities(ctx, dealID)
	if err != nil {
		return domain.Conversation{}, err
	}
	var activityMessages []domain.Message
	for _, activity := range activities {
		messages, err := t.sessionMessages(ctx, activity.SessionID)
		if err != nil {
			if errors.Is(err, domain.ErrConversationAccessDenied) {
				var accessErr *domain.ConversationAccessError
				if errors.As(err, &accessErr) {
					return domain.Conversation{Status: domain.ConversationAccessDenied, SessionID: accessErr.SessionID, ErrorCode: accessErr.Code}, nil
				}
				return domain.Conversation{Status: domain.ConversationAccessDenied, SessionID: activity.SessionID, ErrorCode: "ACCESS_DENIED"}, nil
			}
			return domain.Conversation{}, fmt.Errorf("ler histórico da sessão IMOPENLINES %s: %w", activity.SessionID, err)
		}
		activityMessages = append(activityMessages, messages...)
	}
	linkedMessages, err := t.Messages(ctx, dealID)
	if err != nil {
		if errors.Is(err, platformbitrix.ErrAccessDenied) {
			var apiErr *platformbitrix.APIError
			code := "ACCESS_DENIED"
			if errors.As(err, &apiErr) {
				code = apiErr.Code
			}
			return domain.Conversation{Status: domain.ConversationAccessDenied, ErrorCode: code}, nil
		}
		return domain.Conversation{}, err
	}
	messages := append(activityMessages, linkedMessages...)
	if len(messages) > 0 {
		return domain.Conversation{Status: domain.ConversationAvailable, Messages: messages}, nil
	}
	if len(activities) > 0 {
		return domain.Conversation{Status: domain.ConversationEmpty, SessionID: activities[0].SessionID}, nil
	}
	located, err := t.hasLinkedConversation(ctx, dealID)
	if err != nil {
		return domain.Conversation{}, err
	}
	if located {
		return domain.Conversation{Status: domain.ConversationEmpty}, nil
	}
	return domain.Conversation{Status: domain.ConversationNoConversation}, nil
}

func (t Timeline) openLinesActivities(ctx context.Context, dealID string) ([]openLinesActivity, error) {
	entityID, err := strconv.ParseInt(dealID, 10, 64)
	if err != nil || entityID <= 0 {
		return nil, fmt.Errorf("ID do negócio inválido: %q", dealID)
	}
	var response struct {
		Result []struct {
			ID                 json.RawMessage `json:"ID"`
			AssociatedEntityID json.RawMessage `json:"ASSOCIATED_ENTITY_ID"`
			OriginID           string          `json:"ORIGIN_ID"`
		} `json:"result"`
	}
	if err := t.call(ctx, "crm.activity.list.json", map[string]any{
		"filter": map[string]any{"OWNER_TYPE_ID": 2, "OWNER_ID": entityID, "PROVIDER_ID": "IMOPENLINES_SESSION"},
		"select": []string{"ID", "PROVIDER_ID", "ASSOCIATED_ENTITY_ID", "ORIGIN_ID"},
	}, &response); err != nil {
		return nil, fmt.Errorf("listar atividades IMOPENLINES_SESSION: %w", err)
	}
	seen := map[string]struct{}{}
	var activities []openLinesActivity
	for _, item := range response.Result {
		sessionID, _ := scalarString(item.AssociatedEntityID)
		if sessionID == "" || sessionID == "0" {
			sessionID = strings.TrimPrefix(strings.TrimSpace(item.OriginID), "IMOL_")
		}
		if sessionID == "" || sessionID == "0" {
			continue
		}
		if _, duplicate := seen[sessionID]; duplicate {
			continue
		}
		seen[sessionID] = struct{}{}
		activityID, _ := scalarString(item.ID)
		activities = append(activities, openLinesActivity{ID: activityID, SessionID: sessionID})
	}
	return activities, nil
}

func (t Timeline) hasLinkedConversation(ctx context.Context, dealID string) (bool, error) {
	entityID, _ := strconv.ParseInt(dealID, 10, 64)
	chats, err := t.linkedChats(ctx, map[string]any{"CRM_ENTITY_TYPE": "deal", "CRM_ENTITY": entityID, "ACTIVE_ONLY": "N"}, "deal")
	return len(chats) > 0, err
}

func (t Timeline) Forms(ctx context.Context, dealID string) ([]domain.CRMForm, error) {
	if t.Webhook == "" {
		return nil, fmt.Errorf("BITRIX_WEBHOOK_URL não configurado")
	}
	entityID, err := strconv.ParseInt(dealID, 10, 64)
	if err != nil || entityID <= 0 {
		return nil, fmt.Errorf("ID do negócio inválido: %q", dealID)
	}

	seen := make(map[string]struct{})
	var forms []domain.CRMForm
	start := 0
	for {
		var response struct {
			Result []struct {
				ID             json.RawMessage `json:"ID"`
				ProviderParams json.RawMessage `json:"PROVIDER_PARAMS"`
			} `json:"result"`
			Next *int `json:"next"`
		}
		if err := t.call(ctx, "crm.activity.list.json", map[string]any{
			"filter": map[string]any{
				"OWNER_TYPE_ID": 2,
				"OWNER_ID":      entityID,
				"PROVIDER_ID":   "CRM_WEBFORM",
			},
			"select": []string{"*"},
			"start":  start,
		}, &response); err != nil {
			return nil, fmt.Errorf("listar atividades CRM_WEBFORM: %w", err)
		}
		for index, activity := range response.Result {
			id, err := scalarString(activity.ID)
			if err != nil || id == "" {
				id = fmt.Sprintf("sem-id-%d-%d", start, index)
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			form, err := decodeCRMForm(id, activity.ProviderParams)
			if err != nil {
				return nil, fmt.Errorf("decodificar formulário da atividade %s: %w", id, err)
			}
			forms = append(forms, form)
		}
		if response.Next == nil {
			break
		}
		start = *response.Next
	}
	return forms, nil
}

func decodeCRMForm(activityID string, raw json.RawMessage) (domain.CRMForm, error) {
	form := domain.CRMForm{ActivityID: activityID}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return form, nil
	}
	var params struct {
		Fields       json.RawMessage `json:"FIELDS"`
		Form         json.RawMessage `json:"FORM"`
		VisitedPages json.RawMessage `json:"VISITED_PAGES"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return domain.CRMForm{}, err
	}
	var err error
	if form.Form, err = decodeJSONValue(params.Form); err != nil {
		return domain.CRMForm{}, fmt.Errorf("FORM: %w", err)
	}
	if form.VisitedPages, err = decodeJSONValue(params.VisitedPages); err != nil {
		return domain.CRMForm{}, fmt.Errorf("VISITED_PAGES: %w", err)
	}
	form.Fields, err = decodeCRMFormFields(params.Fields)
	if err != nil {
		return domain.CRMForm{}, fmt.Errorf("FIELDS: %w", err)
	}
	return form, nil
}

func decodeCRMFormFields(raw json.RawMessage) ([]domain.CRMFormField, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var fields []struct {
		Type    string          `json:"type"`
		Code    string          `json:"code"`
		Caption string          `json:"caption"`
		Value   json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	result := make([]domain.CRMFormField, 0, len(fields))
	for _, field := range fields {
		value, err := decodeJSONValue(field.Value)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.CRMFormField{Type: field.Type, Code: field.Code, Caption: field.Caption, Value: value})
	}
	return result, nil
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (t Timeline) Status(ctx context.Context, dealID string) (domain.DealStatus, error) {
	if t.Webhook == "" {
		return domain.DealStatus{}, fmt.Errorf("BITRIX_WEBHOOK_URL não configurado")
	}
	entityID, err := strconv.ParseInt(dealID, 10, 64)
	if err != nil || entityID <= 0 {
		return domain.DealStatus{}, fmt.Errorf("ID do negócio inválido: %q", dealID)
	}
	var response struct {
		Result struct {
			StageID      json.RawMessage `json:"STAGE_ID"`
			SemanticID   json.RawMessage `json:"STAGE_SEMANTIC_ID"`
			Closed       json.RawMessage `json:"CLOSED"`
			AssignedByID json.RawMessage `json:"ASSIGNED_BY_ID"`
		} `json:"result"`
	}
	if err := t.call(ctx, "crm.deal.get.json", map[string]any{"ID": entityID}, &response); err != nil {
		return domain.DealStatus{}, fmt.Errorf("ler estágio do negócio: %w", err)
	}
	stageID, err := scalarString(response.Result.StageID)
	if err != nil {
		return domain.DealStatus{}, fmt.Errorf("STAGE_ID inválido: %w", err)
	}
	semanticID, err := scalarString(response.Result.SemanticID)
	if err != nil {
		return domain.DealStatus{}, fmt.Errorf("STAGE_SEMANTIC_ID inválido: %w", err)
	}
	semanticID = strings.ToUpper(strings.TrimSpace(semanticID))
	if semanticID != "P" && semanticID != "S" && semanticID != "F" {
		return domain.DealStatus{}, fmt.Errorf("STAGE_SEMANTIC_ID inválido: %q", semanticID)
	}
	closed, err := bitrixBool(response.Result.Closed)
	if err != nil {
		return domain.DealStatus{}, fmt.Errorf("CLOSED inválido: %w", err)
	}
	var assignedByID int64
	if len(response.Result.AssignedByID) > 0 && string(response.Result.AssignedByID) != "null" {
		assigned, assignedErr := scalarString(response.Result.AssignedByID)
		if assignedErr != nil {
			return domain.DealStatus{}, fmt.Errorf("ASSIGNED_BY_ID inválido: %w", assignedErr)
		}
		assignedByID, assignedErr = strconv.ParseInt(strings.TrimSpace(assigned), 10, 64)
		if assignedErr != nil || assignedByID <= 0 {
			return domain.DealStatus{}, fmt.Errorf("ASSIGNED_BY_ID inválido")
		}
	}
	return domain.DealStatus{StageID: stageID, SemanticID: semanticID, Closed: closed, AssignedByID: assignedByID}, nil
}

func bitrixBool(raw json.RawMessage) (bool, error) {
	var boolean bool
	if err := json.Unmarshal(raw, &boolean); err == nil {
		return boolean, nil
	}
	value, err := scalarString(raw)
	if err != nil {
		return false, err
	}
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "Y", "1", "TRUE":
		return true, nil
	case "N", "0", "FALSE":
		return false, nil
	default:
		return false, fmt.Errorf("valor booleano desconhecido: %q", value)
	}
}

// Messages returns the history of every Open Lines chat linked to the deal.
// ACTIVE_ONLY must be N: closed chats are the common case during an audit.
func (t Timeline) Messages(ctx context.Context, dealID string) ([]domain.Message, error) {
	if t.Webhook == "" {
		return nil, fmt.Errorf("BITRIX_WEBHOOK_URL não configurado")
	}
	entityID, err := strconv.ParseInt(dealID, 10, 64)
	if err != nil || entityID <= 0 {
		return nil, fmt.Errorf("ID do negócio inválido: %q", dealID)
	}

	dealChats, err := t.linkedChats(ctx, map[string]any{
		"CRM_ENTITY_TYPE": "deal",
		"CRM_ENTITY":      entityID,
		"ACTIVE_ONLY":     "N",
	}, "deal")
	if err != nil {
		return nil, fmt.Errorf("listar chats IMOPENLINES: %w", err)
	}

	var dealResponse struct {
		Result struct {
			ContactID  json.RawMessage `json:"CONTACT_ID"`
			DateCreate string          `json:"DATE_CREATE"`
			CloseDate  string          `json:"CLOSEDATE"`
		} `json:"result"`
	}
	if err := t.call(ctx, "crm.deal.get.json", map[string]any{"ID": entityID}, &dealResponse); err != nil {
		return nil, fmt.Errorf("ler dados do negócio: %w", err)
	}
	periodStart, err := parseOptionalBitrixTime(dealResponse.Result.DateCreate)
	if err != nil {
		return nil, fmt.Errorf("DATE_CREATE inválido: %w", err)
	}
	periodEnd, err := parseOptionalBitrixTime(dealResponse.Result.CloseDate)
	if err != nil {
		return nil, fmt.Errorf("CLOSEDATE inválido: %w", err)
	}

	chats := dealChats
	contactID, _ := scalarString(dealResponse.Result.ContactID)
	if contactID != "" && contactID != "0" {
		contactChats, err := t.linkedChats(ctx, map[string]any{
			"CRM_ENTITY_TYPE": "contact",
			"CRM_ENTITY":      contactID,
			"ACTIVE_ONLY":     "N",
		}, "contact")
		if err != nil {
			return nil, fmt.Errorf("listar chats IMOPENLINES do contato: %w", err)
		}
		chats = append(chats, contactChats...)
	}

	seenChats := make(map[string]struct{}, len(chats))
	seenMessages := make(map[string]struct{})
	var messages []domain.Message
	for _, chat := range chats {
		if _, duplicate := seenChats[chat.ID]; duplicate {
			continue
		}
		seenChats[chat.ID] = struct{}{}

		var history struct {
			Result struct {
				Messages map[string]struct {
					ID       json.RawMessage `json:"id"`
					SenderID json.RawMessage `json:"senderId"`
					Date     string          `json:"date"`
					Text     string          `json:"text"`
					Params   json.RawMessage `json:"params"`
				} `json:"message"`
				Files   historyFiles `json:"files"`
				Users   historyUsers `json:"users"`
				Session struct {
					ManagerList json.RawMessage `json:"managerList"`
				} `json:"session"`
			} `json:"result"`
		}
		if err := t.call(ctx, "imopenlines.session.history.get.json", map[string]any{"CHAT_ID": chat.ID}, &history); err != nil {
			return nil, fmt.Errorf("ler histórico do chat IMOPENLINES %s: %w", chat.ID, err)
		}
		managers := idSet(history.Result.Session.ManagerList)
		for mapID, item := range history.Result.Messages {
			id, _ := scalarString(item.ID)
			if id == "" {
				// The history response is keyed by the original Bitrix message ID.
				// This is a source-provided fallback, not a generated identifier.
				id = mapID
			}
			if strings.TrimSpace(id) == "" || id == "0" {
				return nil, fmt.Errorf("mensagem sem ID válido no chat IMOPENLINES %s", chat.ID)
			}
			senderID, _ := scalarString(item.SenderID)
			if senderID == "" || senderID == "0" {
				continue
			}
			user, ok := history.Result.Users[senderID]
			if !ok {
				user, ok = history.Result.Users["user"+senderID]
			}
			role := classifySender(senderID, user, ok, managers)
			if role == "" { // bots/system senders and unresolved integrations are not human
				continue
			}
			// IDs are portal-wide. A repeated ID in another chat is still the same message.
			if _, duplicate := seenMessages[id]; duplicate {
				continue
			}
			seenMessages[id] = struct{}{}
			attachments := referencedAttachments(item.Params, history.Result.Files)
			text := Sanitize(item.Text)
			if text == "" && len(attachments) == 0 {
				continue
			}
			created, err := time.Parse(time.RFC3339, item.Date)
			if err != nil {
				return nil, fmt.Errorf("data inválida na mensagem %s do chat %s: %w", id, chat.ID, err)
			}
			if chat.Origin == "contact" && !withinDealPeriod(created, periodStart, periodEnd) {
				continue
			}
			messages = append(messages, domain.Message{
				ID:          id,
				SessionID:   chat.ID,
				SenderID:    senderID,
				SenderName:  senderName(user),
				CreatedAt:   created,
				Text:        text,
				Role:        role,
				ChatOrigin:  chat.Origin,
				Attachments: attachments,
			})
		}
	}

	sort.SliceStable(messages, func(i, j int) bool {
		if messages[i].CreatedAt.Equal(messages[j].CreatedAt) {
			return messages[i].ID < messages[j].ID
		}
		return messages[i].CreatedAt.Before(messages[j].CreatedAt)
	})
	return messages, nil
}

func (t Timeline) sessionMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	var history struct {
		Result struct {
			Messages map[string]struct {
				ID       json.RawMessage `json:"id"`
				SenderID json.RawMessage `json:"senderId"`
				Date     string          `json:"date"`
				Text     string          `json:"text"`
				Params   json.RawMessage `json:"params"`
			} `json:"message"`
			Files   historyFiles `json:"files"`
			Users   historyUsers `json:"users"`
			Session struct {
				ManagerList json.RawMessage `json:"managerList"`
			} `json:"session"`
		} `json:"result"`
	}
	// ASSOCIATED_ENTITY_ID is a SESSION_ID in IMOPENLINES_SESSION activities.
	// It must never be sent as CHAT_ID.
	if err := t.call(ctx, "imopenlines.session.history.get.json", map[string]any{"SESSION_ID": sessionID}, &history); err != nil {
		if errors.Is(err, platformbitrix.ErrAccessDenied) {
			var apiErr *platformbitrix.APIError
			code := "ACCESS_DENIED"
			if errors.As(err, &apiErr) {
				code = apiErr.Code
			}
			return nil, &domain.ConversationAccessError{SessionID: sessionID, Code: code}
		}
		return nil, err
	}
	managers := idSet(history.Result.Session.ManagerList)
	messages := make([]domain.Message, 0, len(history.Result.Messages))
	for mapID, item := range history.Result.Messages {
		id, _ := scalarString(item.ID)
		if id == "" {
			id = mapID
		}
		senderID, _ := scalarString(item.SenderID)
		if strings.TrimSpace(id) == "" || senderID == "" || senderID == "0" {
			continue
		}
		user, found := history.Result.Users[senderID]
		if !found {
			user, found = history.Result.Users["user"+senderID]
		}
		role := classifySender(senderID, user, found, managers)
		if role == "" {
			continue
		}
		attachments := referencedAttachments(item.Params, history.Result.Files)
		text := Sanitize(item.Text)
		if text == "" && len(attachments) == 0 {
			continue
		}
		created, err := time.Parse(time.RFC3339, item.Date)
		if err != nil {
			return nil, fmt.Errorf("data inválida na mensagem %s da sessão %s: %w", id, sessionID, err)
		}
		messages = append(messages, domain.Message{ID: id, SessionID: sessionID, SenderID: senderID, SenderName: senderName(user), CreatedAt: created, Text: text, Role: role, ChatOrigin: "deal", Attachments: attachments})
	}
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].CreatedAt.Before(messages[j].CreatedAt) })
	return messages, nil
}

func senderName(user historyUser) string {
	if name := strings.TrimSpace(user.Name); name != "" {
		return name
	}
	return strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
}

type linkedChat struct {
	ID     string
	Origin string
}

func (t Timeline) linkedChats(ctx context.Context, payload map[string]any, origin string) ([]linkedChat, error) {
	var response struct {
		Result []struct {
			ChatID json.RawMessage `json:"CHAT_ID"`
		} `json:"result"`
	}
	if err := t.call(ctx, "imopenlines.crm.chat.get.json", payload, &response); err != nil {
		return nil, err
	}
	var chats []linkedChat
	for _, item := range response.Result {
		id, err := scalarString(item.ChatID)
		if err != nil || id == "" || id == "0" {
			continue
		}
		chats = append(chats, linkedChat{ID: id, Origin: origin})
	}
	return chats, nil
}

func parseOptionalBitrixTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("formato de data desconhecido: %q", value)
}

func withinDealPeriod(created time.Time, start, end *time.Time) bool {
	if start != nil && created.Before(*start) {
		return false
	}
	if end != nil && created.After(*end) {
		return false
	}
	return true
}

var imageExtension = regexp.MustCompile(`(?i)\.(?:png|jpe?g|webp)(?:$|[?#])`)

type historyFile struct {
	ID   json.RawMessage `json:"id"`
	Name string          `json:"name"`
	Type string          `json:"type"`
}

type historyFiles map[string]historyFile

func (files *historyFiles) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*files = nil
		return nil
	}
	result := make(historyFiles)
	if data[0] == '{' {
		var object map[string]historyFile
		if err := json.Unmarshal(data, &object); err != nil {
			return fmt.Errorf("decodificar arquivos do histórico: %w", err)
		}
		for key, file := range object {
			id, _ := scalarString(file.ID)
			if id == "" {
				id = key
			}
			result[id] = file
		}
		*files = result
		return nil
	}
	if data[0] != '[' {
		return fmt.Errorf("decodificar arquivos do histórico: esperado objeto ou array")
	}
	var array []historyFile
	if err := json.Unmarshal(data, &array); err != nil {
		return fmt.Errorf("decodificar array de arquivos do histórico: %w", err)
	}
	for index, file := range array {
		id, _ := scalarString(file.ID)
		if id == "" {
			id = strconv.Itoa(index)
		}
		result[id] = file
	}
	*files = result
	return nil
}

func referencedAttachments(params json.RawMessage, files historyFiles) []domain.Attachment {
	var decoded map[string]json.RawMessage
	if len(params) == 0 || json.Unmarshal(params, &decoded) != nil {
		return nil
	}
	var rawIDs json.RawMessage
	for key, value := range decoded {
		if strings.EqualFold(key, "fileId") {
			rawIDs = value
			break
		}
	}
	ids := scalarIDList(rawIDs)
	attachments := make([]domain.Attachment, 0, len(ids))
	for _, id := range ids {
		file, ok := files[id]
		if !ok {
			continue
		}
		isImage := strings.EqualFold(file.Type, "image") || imageExtension.MatchString(file.Name)
		attachments = append(attachments, domain.Attachment{ID: id, Name: file.Name, MIMEType: file.Type, IsImage: isImage})
	}
	return attachments
}

func scalarIDList(raw json.RawMessage) []string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var ids []string
	var visit func(any)
	visit = func(item any) {
		switch item := item.(type) {
		case string:
			ids = append(ids, item)
		case float64:
			ids = append(ids, strconv.FormatFloat(item, 'f', -1, 64))
		case []any:
			for _, child := range item {
				visit(child)
			}
		case map[string]any:
			for key, child := range item {
				if _, exists := filesafeScalar(child); !exists {
					ids = append(ids, key)
				}
				visit(child)
			}
		}
	}
	visit(value)
	return ids
}

func filesafeScalar(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	default:
		return "", false
	}
}

type historyUser struct {
	ID             json.RawMessage `json:"id"`
	Name           string          `json:"name"`
	FirstName      string          `json:"first_name"`
	LastName       string          `json:"last_name"`
	Type           string          `json:"type"`
	ExternalAuthID string          `json:"external_auth_id"`
	Extranet       any             `json:"extranet"`
	Connector      any             `json:"connector"`
	Phone          string          `json:"phone"`
	PersonalPhone  string          `json:"personal_phone"`
	PersonalMobile string          `json:"personal_mobile"`
	WorkPhone      string          `json:"work_phone"`
}

type historyUsers map[string]historyUser

func (users *historyUsers) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*users = nil
		return nil
	}
	if len(data) == 0 {
		return fmt.Errorf("decodificar usu\u00e1rios do hist\u00f3rico: JSON vazio")
	}

	if data[0] == '{' {
		var object map[string]historyUser
		if err := json.Unmarshal(data, &object); err != nil {
			return fmt.Errorf("decodificar objeto de usu\u00e1rios do hist\u00f3rico: %w", err)
		}
		*users = object
		return nil
	}
	if data[0] != '[' {
		return fmt.Errorf("decodificar usu\u00e1rios do hist\u00f3rico: esperado objeto ou array")
	}

	var array []historyUser
	if err := json.Unmarshal(data, &array); err != nil {
		return fmt.Errorf("decodificar usu\u00e1rios do hist\u00f3rico: %w", err)
	}

	normalized := make(historyUsers, len(array))
	for index, user := range array {
		id, err := scalarString(user.ID)
		if err != nil {
			return fmt.Errorf("decodificar ID do usu\u00e1rio na posi\u00e7\u00e3o %d: %w", index, err)
		}
		if id == "" {
			return fmt.Errorf("usu\u00e1rio na posi\u00e7\u00e3o %d sem ID", index)
		}
		normalized[id] = user
	}
	*users = normalized
	return nil
}

func classifySender(id string, user historyUser, found bool, managers map[string]struct{}) string {
	if _, ok := managers[id]; ok {
		return "ATENDENTE"
	}
	if !found {
		return ""
	}
	kind := strings.ToLower(user.Type + " " + user.ExternalAuthID + " " + fmt.Sprint(user.Extranet) + " " + fmt.Sprint(user.Connector))
	if strings.Contains(kind, "bot") || strings.Contains(kind, "system") {
		return ""
	}
	if strings.Contains(kind, "connector") || strings.Contains(kind, "extranet") || strings.Contains(kind, "external") || strings.Contains(kind, "true") || strings.Contains(kind, "client") {
		return "CLIENTE"
	}
	if strings.Contains(kind, "user") || strings.Contains(kind, "employee") || strings.Contains(kind, "internal") {
		return "ATENDENTE"
	}
	return ""
}

func idSet(raw json.RawMessage) map[string]struct{} {
	result := make(map[string]struct{})
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return result
	}
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				visit(item)
			}
		case map[string]any:
			for key, item := range x {
				if key != "" {
					result[key] = struct{}{}
				}
				visit(item)
			}
		case string:
			result[x] = struct{}{}
		case float64:
			result[strconv.FormatFloat(x, 'f', -1, 64)] = struct{}{}
		}
	}
	visit(value)
	return result
}

func (t Timeline) call(ctx context.Context, method string, payload any, target any) error {
	return (platformbitrix.Client{Webhook: t.Webhook, HTTP: t.Client}).Call(ctx, method, payload, target)
}

func scalarString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), nil
	}
	return "", fmt.Errorf("valor não é texto nem número")
}

func Sanitize(raw string) string {
	return privacy.Sanitize(raw)
}
