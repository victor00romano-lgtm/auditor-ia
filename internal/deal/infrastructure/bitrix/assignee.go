package bitrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	platformbitrix "github.com/portfolio/auditor-ia/internal/platform/bitrix"
)

func GetAssignee(ctx context.Context, webhook string, client *http.Client, id int64) (dealdomain.Assignee, error) {
	if id <= 0 {
		return dealdomain.Assignee{}, fmt.Errorf("ID de responsável inválido")
	}
	var response struct {
		Result []struct {
			ID         string          `json:"ID"`
			Name       string          `json:"NAME"`
			LastName   string          `json:"LAST_NAME"`
			SecondName string          `json:"SECOND_NAME"`
			Active     json.RawMessage `json:"ACTIVE"`
		} `json:"result"`
	}
	if err := (platformbitrix.Client{Webhook: webhook, HTTP: client}).Call(ctx, "user.get.json", map[string]any{"ID": id}, &response); err != nil {
		return dealdomain.Assignee{}, fmt.Errorf("buscar usuário Bitrix %d: %w", id, err)
	}
	if len(response.Result) == 0 {
		return dealdomain.Assignee{}, fmt.Errorf("usuário Bitrix %d não encontrado", id)
	}
	user := response.Result[0]
	name := strings.Join(strings.Fields(strings.TrimSpace(user.Name+" "+user.SecondName+" "+user.LastName)), " ")
	if name == "" {
		name = fmt.Sprintf("Usuário Bitrix #%d", id)
	}
	active, err := optionalBool(user.Active, "ACTIVE")
	if err != nil {
		return dealdomain.Assignee{}, fmt.Errorf("usuário Bitrix %d: %w", id, err)
	}
	return dealdomain.Assignee{BitrixUserID: id, DisplayName: name, Active: active, SyncedAt: time.Now().UTC()}, nil
}
