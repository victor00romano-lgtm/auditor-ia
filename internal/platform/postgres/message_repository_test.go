package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

type messageExecCall struct {
	SQL  string
	Args []any
}

type messageTxStub struct {
	calls      []messageExecCall
	errors     []error
	committed  bool
	rolledBack bool
}

func (tx *messageTxStub) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.calls = append(tx.calls, messageExecCall{SQL: sql, Args: args})
	var err error
	if len(tx.errors) > 0 {
		err = tx.errors[0]
		tx.errors = tx.errors[1:]
	}
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *messageTxStub) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *messageTxStub) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type messageTxStarterStub struct {
	tx  messageTx
	err error
}

func (starter messageTxStarterStub) Begin(context.Context) (messageTx, error) {
	return starter.tx, starter.err
}

func TestMessageRepositoryMapsIDsOrdersChronologicallyAndSerializesAttachments(t *testing.T) {
	tx := &messageTxStub{}
	repository := &MessageRepository{db: messageTxStarterStub{tx: tx}}
	earlier := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)
	messages := []conversationdomain.Message{
		{ID: "2", SessionID: "chat-20", SenderID: "8", SenderName: "Atendente", Role: "ATENDENTE", Text: "depois", CreatedAt: later},
		{ID: "1", SessionID: "chat-20", SenderID: "55", SenderName: "Cliente", Role: "CLIENTE", Text: "", CreatedAt: earlier, Attachments: []conversationdomain.Attachment{{ID: "900", Name: "imagem.png", MIMEType: "image", IsImage: true}}},
	}

	persisted, err := repository.UpsertBatch(context.Background(), 71, messages)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Processed != 2 || len(tx.calls) != 2 || !tx.committed || tx.rolledBack {
		t.Fatalf("transação inesperada: persisted=%d calls=%d commit=%v rollback=%v", persisted.Processed, len(tx.calls), tx.committed, tx.rolledBack)
	}
	first := tx.calls[0]
	if first.Args[0] != int64(71) || first.Args[1] != "1" || first.Args[2] != "chat-20" || first.Args[3] != "55" || first.Args[4] != "CLIENTE" || first.Args[5] != "Cliente" || first.Args[6] != "" || first.Args[7] != earlier {
		t.Fatalf("mapeamento/ordem inesperados: %#v", first.Args)
	}
	encoded, ok := first.Args[8].([]byte)
	if !ok || !json.Valid(encoded) || !strings.Contains(string(encoded), `"id":"900"`) || !strings.Contains(string(encoded), `"is_image":true`) {
		t.Fatalf("JSONB de anexos inválido: %#v", first.Args[8])
	}
	if tx.calls[1].Args[1] != "2" || !strings.Contains(first.SQL, "ON CONFLICT (session_id, bitrix_message_id) DO UPDATE") {
		t.Fatalf("ordem ou UPSERT incorreto: %#v", tx.calls)
	}
}

func TestMessageRepositoryRollsBackWholeBatchOnError(t *testing.T) {
	tx := &messageTxStub{errors: []error{nil, errors.New("falha simulada")}}
	repository := &MessageRepository{db: messageTxStarterStub{tx: tx}}
	messages := []conversationdomain.Message{
		{ID: "1", SessionID: "10", Role: "CLIENTE"},
		{ID: "2", SessionID: "10", Role: "ATENDENTE"},
	}

	persisted, err := repository.UpsertBatch(context.Background(), 71, messages)
	if err == nil || persisted.Processed != 0 {
		t.Fatalf("resultado inesperado: persisted=%d err=%v", persisted.Processed, err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("lote parcial não sofreu rollback: commit=%v rollback=%v", tx.committed, tx.rolledBack)
	}
}

func TestMessageRepositoryDuplicateUpdatesWithoutIncreasingCountWithPostgreSQL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		t.Skip("DATABASE_URL não configurada; teste de integração ignorado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	bitrixDealID := time.Now().UnixNano()
	title := "Teste de mensagens"
	dealID, err := NewDealRepository(pool).Upsert(ctx, dealdomain.Deal{BitrixDealID: bitrixDealID, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM deals WHERE id = $1`, dealID)
	}()

	repository := NewMessageRepository(pool)
	sentAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	message := conversationdomain.Message{ID: "msg-1", SessionID: "session-1", SenderID: "55", SenderName: "Cliente", Role: "CLIENTE", Text: "original", CreatedAt: sentAt}
	firstPersisted, err := repository.UpsertBatch(ctx, dealID, []conversationdomain.Message{message})
	if err != nil {
		t.Fatal(err)
	}
	message.Text = "atualizada"
	message.SenderName = "Cliente Atualizado"
	secondPersisted, err := repository.UpsertBatch(ctx, dealID, []conversationdomain.Message{message})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	var text, senderName string
	err = pool.QueryRow(ctx, `SELECT COUNT(*), MAX(message_text), MAX(sender_name) FROM conversation_messages WHERE deal_id = $1`, dealID).
		Scan(&count, &text, &senderName)
	if err != nil {
		t.Fatal(err)
	}
	if firstPersisted.Processed != 1 || secondPersisted.Processed != 1 || count != 1 || text != "atualizada" || senderName != "Cliente Atualizado" {
		t.Fatalf("UPSERT inesperado: first=%d second=%d count=%d text=%q sender=%q", firstPersisted.Processed, secondPersisted.Processed, count, text, senderName)
	}
}
