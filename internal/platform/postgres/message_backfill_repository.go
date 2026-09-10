package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/portfolio/auditor-ia/internal/messagebackfill"
)

type MessageBackfillRepository struct{ pool *pgxpool.Pool }

func NewMessageBackfillRepository(pool *pgxpool.Pool) *MessageBackfillRepository {
	return &MessageBackfillRepository{pool: pool}
}

func (r *MessageBackfillRepository) Select(ctx context.Context, selection messagebackfill.Selection) ([]messagebackfill.Deal, error) {
	rows, err := r.pool.Query(ctx, `
SELECT d.id,d.bitrix_deal_id
FROM deals d
WHERE ($1::bigint=0 OR d.bitrix_deal_id=$1)
  AND ($2 OR NOT EXISTS(SELECT 1 FROM conversation_messages m WHERE m.deal_id=d.id))
ORDER BY d.bitrix_deal_id
LIMIT $3`, selection.DealID, selection.IncludeWithMessages, selection.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deals []messagebackfill.Deal
	for rows.Next() {
		var deal messagebackfill.Deal
		if err := rows.Scan(&deal.InternalID, &deal.BitrixID); err != nil {
			return nil, err
		}
		deals = append(deals, deal)
	}
	return deals, rows.Err()
}

var _ messagebackfill.DealRepository = (*MessageBackfillRepository)(nil)
