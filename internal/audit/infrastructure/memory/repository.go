package memory

import (
	"context"
	"errors"
	"sync"

	"github.com/portfolio/auditor-ia/internal/audit/domain"
)

type Repository struct {
	mu   sync.RWMutex
	data map[string]*domain.Audit
}

func New() *Repository { return &Repository{data: map[string]*domain.Audit{}} }
func (r *Repository) Save(_ context.Context, a *domain.Audit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *a
	r.data[a.ID] = &copy
	return nil
}
func (r *Repository) Find(_ context.Context, id string) (*domain.Audit, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.data[id]
	if !ok {
		return nil, errors.New("auditoria não encontrada")
	}
	copy := *a
	return &copy, nil
}
func (r *Repository) List(_ context.Context) ([]*domain.Audit, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.Audit, 0, len(r.data))
	for _, a := range r.data {
		copy := *a
		out = append(out, &copy)
	}
	return out, nil
}
