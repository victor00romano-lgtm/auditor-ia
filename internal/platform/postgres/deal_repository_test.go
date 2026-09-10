package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

type queryCall struct {
	SQL  string
	Args []any
}

type queryRecorder struct {
	calls []queryCall
	rows  []pgx.Row
}

func (recorder *queryRecorder) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	recorder.calls = append(recorder.calls, queryCall{SQL: sql, Args: args})
	row := recorder.rows[0]
	recorder.rows = recorder.rows[1:]
	return row
}

type rowResult struct {
	id  int64
	err error
}

func (row rowResult) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*(destinations[0].(*int64)) = row.id
	return nil
}

func TestDealRepositoryUpsertReturnsIDAndUsesConflictClause(t *testing.T) {
	recorder := &queryRecorder{rows: []pgx.Row{rowResult{id: 71}, rowResult{id: 71}}}
	repository := &DealRepository{db: recorder}
	title := "Primeiro título"
	updatedTitle := "Título atualizado"
	closed := false

	insertID, err := repository.Upsert(context.Background(), dealdomain.Deal{BitrixDealID: 3374, Title: &title, Closed: &closed})
	if err != nil {
		t.Fatal(err)
	}
	updateID, err := repository.Upsert(context.Background(), dealdomain.Deal{BitrixDealID: 3374, Title: &updatedTitle})
	if err != nil {
		t.Fatal(err)
	}
	if insertID != 71 || updateID != insertID || len(recorder.calls) != 2 {
		t.Fatalf("IDs/chamadas inesperados: insert=%d update=%d chamadas=%d", insertID, updateID, len(recorder.calls))
	}
	for _, call := range recorder.calls {
		if !strings.Contains(call.SQL, "ON CONFLICT (bitrix_deal_id) DO UPDATE") || !strings.Contains(call.SQL, "RETURNING id") || len(call.Args) != 11 {
			t.Fatalf("UPSERT inesperado: SQL=%q args=%#v", call.SQL, call.Args)
		}
	}
	if recorder.calls[1].Args[4] != (*bool)(nil) {
		t.Fatalf("campo opcional ausente foi convertido em falso: %#v", recorder.calls[1].Args[4])
	}
}

func TestDealRepositoryUpsertPropagatesErrorWithoutCredentials(t *testing.T) {
	const databaseURL = "postgres://usuario:senha-secreta@localhost:5432/auditor"
	t.Setenv("DATABASE_URL", databaseURL)
	recorder := &queryRecorder{rows: []pgx.Row{rowResult{err: errors.New("falha em " + databaseURL)}}}

	_, err := (&DealRepository{db: recorder}).Upsert(context.Background(), dealdomain.Deal{BitrixDealID: 3374})
	if err == nil || !strings.Contains(err.Error(), "persistir negócio no PostgreSQL") {
		t.Fatalf("erro inesperado: %v", err)
	}
	if strings.Contains(err.Error(), databaseURL) || strings.Contains(err.Error(), "senha-secreta") {
		t.Fatalf("erro expôs credenciais: %q", err)
	}
}

func TestDealRepositoryUpsertInsertAndUpdateWithPostgreSQL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		t.Skip("DATABASE_URL não configurada; teste de integração ignorado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	repository := &DealRepository{db: tx}
	bitrixID := time.Now().UnixNano()
	title := "Inserido"
	updatedTitle := "Atualizado"
	amount := "125.50"
	closed := false
	updatedClosed := true
	insertedID, err := repository.Upsert(ctx, dealdomain.Deal{BitrixDealID: bitrixID, Title: &title, Amount: &amount, Closed: &closed})
	if err != nil {
		t.Fatal(err)
	}
	updatedID, err := repository.Upsert(ctx, dealdomain.Deal{BitrixDealID: bitrixID, Title: &updatedTitle, Closed: &updatedClosed})
	if err != nil {
		t.Fatal(err)
	}
	if insertedID != updatedID {
		t.Fatalf("UPSERT duplicou o negócio: %d != %d", insertedID, updatedID)
	}

	var count int
	var storedTitle, storedAmount string
	var storedClosed bool
	err = tx.QueryRow(ctx, `SELECT COUNT(*), MAX(title), MAX(amount)::text, BOOL_OR(closed) FROM deals WHERE bitrix_deal_id = $1`, bitrixID).
		Scan(&count, &storedTitle, &storedAmount, &storedClosed)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || storedTitle != updatedTitle || storedAmount != amount || !storedClosed {
		t.Fatalf("estado persistido inesperado: count=%d title=%q amount=%q closed=%v", count, storedTitle, storedAmount, storedClosed)
	}
}
