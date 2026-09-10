package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

const connectionTimeout = 5 * time.Second

// Open creates and validates a PostgreSQL connection pool using DATABASE_URL.
func Open(parent context.Context) (*pgxpool.Pool, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return nil, fmt.Errorf("configurar PostgreSQL: DATABASE_URL não configurada")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, safeConnectionError("configurar pool PostgreSQL", err, databaseURL)
	}

	ctx, cancel := context.WithTimeout(parent, connectionTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, safeConnectionError("abrir pool PostgreSQL", err, databaseURL, config.ConnConfig.Password)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, safeConnectionError("validar conexão PostgreSQL", err, databaseURL, config.ConnConfig.Password)
	}

	return pool, nil
}

func safeConnectionError(operation string, err error, databaseURL string, secrets ...string) error {
	return sanitizedConnectionError{operation: operation, err: err, secrets: append([]string{databaseURL}, secrets...)}
}

type sanitizedConnectionError struct {
	operation string
	err       error
	secrets   []string
}

func (e sanitizedConnectionError) Error() string {
	message := e.err.Error()
	for _, secret := range e.secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redigido]")
		}
	}
	return fmt.Sprintf("%s: %s", e.operation, security.Text(message))
}
func (e sanitizedConnectionError) Unwrap() error { return e.err }
