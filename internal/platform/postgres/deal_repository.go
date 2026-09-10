package postgres

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type DealRepository struct {
	db rowQuerier
}

func NewDealRepository(pool *pgxpool.Pool) *DealRepository {
	return &DealRepository{db: pool}
}

const upsertDealSQL = `
INSERT INTO deals (
    bitrix_deal_id,
    title,
    stage_id,
    stage_semantic_id,
    closed,
    amount,
    currency,
    assigned_by_id,
    created_at_bitrix,
    updated_at_bitrix,
    synced_at
) VALUES (
    $1,
    COALESCE($2, ''),
    COALESCE($3, ''),
    COALESCE($4, ''),
    COALESCE($5, FALSE),
    $6::numeric,
    $7,
    $8,
    $9,
    $10,
    COALESCE($11, NOW())
)
ON CONFLICT (bitrix_deal_id) DO UPDATE SET
    title = COALESCE($2, deals.title),
    stage_id = COALESCE($3, deals.stage_id),
    stage_semantic_id = COALESCE($4, deals.stage_semantic_id),
    closed = COALESCE($5, deals.closed),
    amount = COALESCE($6::numeric, deals.amount),
    currency = COALESCE($7, deals.currency),
    assigned_by_id = COALESCE($8, deals.assigned_by_id),
    created_at_bitrix = COALESCE($9, deals.created_at_bitrix),
    updated_at_bitrix = COALESCE($10, deals.updated_at_bitrix),
    synced_at = COALESCE($11, NOW())
RETURNING id`

func (r *DealRepository) Upsert(ctx context.Context, deal dealdomain.Deal) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("persistir negócio: repositório PostgreSQL não configurado")
	}
	if deal.BitrixDealID <= 0 {
		return 0, fmt.Errorf("persistir negócio: bitrix_deal_id deve ser positivo")
	}

	var id int64
	err := r.db.QueryRow(
		ctx,
		upsertDealSQL,
		deal.BitrixDealID,
		deal.Title,
		deal.StageID,
		deal.StageSemanticID,
		deal.Closed,
		deal.Amount,
		deal.Currency,
		deal.AssignedByID,
		deal.CreatedAtBitrix,
		deal.UpdatedAtBitrix,
		deal.SyncedAt,
	).Scan(&id)
	if err != nil {
		return 0, safeConnectionError("persistir negócio no PostgreSQL", err, os.Getenv("DATABASE_URL"))
	}
	return id, nil
}

var _ dealdomain.DealRepository = (*DealRepository)(nil)
