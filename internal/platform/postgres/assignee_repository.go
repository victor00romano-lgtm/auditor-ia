package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

type AssigneeRepository struct{ pool *pgxpool.Pool }

func NewAssigneeRepository(pool *pgxpool.Pool) *AssigneeRepository {
	return &AssigneeRepository{pool: pool}
}

func (r *AssigneeRepository) MissingIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT d.assigned_by_id FROM deals d LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id WHERE d.assigned_by_id IS NOT NULL AND u.bitrix_user_id IS NULL ORDER BY d.assigned_by_id`)
	if err != nil {
		return nil, fmt.Errorf("listar responsáveis pendentes: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *AssigneeRepository) Upsert(ctx context.Context, value dealdomain.Assignee) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO bitrix_users(bitrix_user_id,display_name,active,synced_at) VALUES($1,$2,$3,$4) ON CONFLICT(bitrix_user_id) DO UPDATE SET display_name=EXCLUDED.display_name,active=EXCLUDED.active,synced_at=EXCLUDED.synced_at`, value.BitrixUserID, value.DisplayName, value.Active, value.SyncedAt)
	if err != nil {
		return fmt.Errorf("persistir responsável: %w", err)
	}
	return nil
}
