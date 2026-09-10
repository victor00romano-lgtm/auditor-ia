package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
)

type messageTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type messageTxStarter interface {
	Begin(context.Context) (messageTx, error)
}

type poolMessageTxStarter struct {
	pool *pgxpool.Pool
}

func (starter poolMessageTxStarter) Begin(ctx context.Context) (messageTx, error) {
	return starter.pool.Begin(ctx)
}

type MessageRepository struct {
	db messageTxStarter
}

func NewMessageRepository(pool *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{db: poolMessageTxStarter{pool: pool}}
}

const upsertMessageSQL = `
INSERT INTO conversation_messages (
    deal_id,
    bitrix_message_id,
    session_id,
    sender_id,
    sender_role,
    sender_name,
    message_text,
    sent_at,
    attachments
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
ON CONFLICT (session_id, bitrix_message_id) DO UPDATE SET
    sender_id = EXCLUDED.sender_id,
    sender_role = EXCLUDED.sender_role,
    sender_name = EXCLUDED.sender_name,
    message_text = EXCLUDED.message_text,
    sent_at = EXCLUDED.sent_at,
    attachments = EXCLUDED.attachments`

func (r *MessageRepository) UpsertBatch(ctx context.Context, dealID int64, messages []conversationdomain.Message) (result conversationdomain.MessageUpsertResult, returnErr error) {
	started := time.Now()
	ctx, span := observability.Tracer().Start(ctx, "postgres.messages.upsert")
	defer func() {
		span.End()
		if metrics := observability.CurrentMetrics(); metrics != nil {
			status := map[bool]string{true: "error", false: "success"}[returnErr != nil]
			metrics.PostgresDuration.WithLabelValues("messages.upsert", status).Observe(time.Since(started).Seconds())
			if returnErr != nil {
				metrics.MessagePersistenceFailures.Inc()
			} else {
				metrics.MessagesPersisted.Add(float64(result.Processed))
			}
		}
	}()
	if r == nil || r.db == nil {
		return result, fmt.Errorf("persistir mensagens: repositório PostgreSQL não configurado")
	}
	if dealID <= 0 {
		return result, fmt.Errorf("persistir mensagens: deal_id deve ser positivo")
	}
	ordered := append([]conversationdomain.Message(nil), messages...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	for index, message := range ordered {
		if strings.TrimSpace(message.ID) == "" || message.ID == "0" {
			return result, fmt.Errorf("persistir mensagens: mensagem na posição %d sem bitrix_message_id válido", index)
		}
		if strings.TrimSpace(message.SessionID) == "" || message.SessionID == "0" {
			return result, fmt.Errorf("persistir mensagens: mensagem na posição %d sem session_id válido", index)
		}
		if message.Role != "CLIENTE" && message.Role != "ATENDENTE" {
			return result, fmt.Errorf("persistir mensagens: mensagem na posição %d com sender_role inválido", index)
		}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return result, safeConnectionError("iniciar transação de mensagens", err, os.Getenv("DATABASE_URL"))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	persisted := 0
	for index, message := range ordered {
		attachments := message.Attachments
		if attachments == nil {
			attachments = []conversationdomain.Attachment{}
		}
		encodedAttachments, err := json.Marshal(attachments)
		if err != nil {
			return result, fmt.Errorf("serializar anexos da mensagem na posição %d: %w", index, err)
		}
		var sentAt any
		if !message.CreatedAt.IsZero() {
			sentAt = message.CreatedAt
		}
		commandTag, err := tx.Exec(
			ctx,
			upsertMessageSQL,
			dealID,
			message.ID,
			message.SessionID,
			nullIfEmpty(message.SenderID),
			message.Role,
			nullIfEmpty(message.SenderName),
			message.Text,
			sentAt,
			encodedAttachments,
		)
		if err != nil {
			return result, safeConnectionError(fmt.Sprintf("persistir mensagem na posição %d", index), err, os.Getenv("DATABASE_URL"))
		}
		persisted += int(commandTag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return result, safeConnectionError("confirmar transação de mensagens", err, os.Getenv("DATABASE_URL"))
	}
	committed = true
	result.Processed = persisted
	return result, nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

var _ conversationdomain.MessageRepository = (*MessageRepository)(nil)
