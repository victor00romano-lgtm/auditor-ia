package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/batch/domain"
)

type Worker struct {
	Queue       domain.Queue
	Processor   domain.Processor
	ID          string
	Lease       time.Duration
	Poll        time.Duration
	MaxAttempts int
	Now         func() time.Time
}

func (w Worker) Run(ctx context.Context) error {
	if w.Queue == nil || w.Processor == nil {
		return fmt.Errorf("worker de lote não configurado")
	}
	if w.ID == "" {
		w.ID = "worker"
	}
	if w.Lease <= 0 {
		w.Lease = 15 * time.Minute
	}
	if w.Poll <= 0 {
		w.Poll = time.Second
	}
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = 5
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	if _, err := w.Queue.RecoverExpired(ctx); err != nil {
		return fmt.Errorf("recuperar leases: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		item, err := w.Queue.Claim(ctx, w.ID, w.Lease)
		if err != nil {
			return fmt.Errorf("reservar item: %w", err)
		}
		if item == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(w.Poll):
				continue
			}
		}
		// A chave identifica o item, não a tentativa. Se o lease expirar depois de a
		// avaliação ser persistida, a retomada reaproveita a avaliação concluída.
		executionCtx := domain.WithExecutionKey(ctx, fmt.Sprintf("batch-item-%d", item.ID))
		assessmentID, runErr := w.Processor.Run(executionCtx, item.BitrixDealID)
		if runErr == nil {
			if err = w.Queue.Complete(ctx, *item, assessmentID); err != nil {
				logTransitionError("complete", *item, err)
			}
			continue
		}
		// O encerramento do processo deixa o item sob lease. Na próxima
		// inicialização RecoverExpired decide entre reagendar ou cancelar, sem
		// gravar uma falha permanente causada apenas pelo shutdown.
		if ctx.Err() != nil {
			return nil
		}
		category, retry := Classify(runErr)
		safeMessage := "Falha temporária durante a auditoria."
		if !retry {
			safeMessage = "Falha permanente durante a auditoria."
		}
		if retry && item.Attempts < w.MaxAttempts {
			delay := RetryDelay(item.Attempts) + time.Duration(rand.IntN(1000))*time.Millisecond
			if err = w.Queue.Reschedule(ctx, *item, w.Now().Add(delay), category, safeMessage); err != nil {
				logTransitionError("reschedule", *item, err)
			}
		} else if err = w.Queue.Fail(ctx, *item, category, safeMessage); err != nil {
			logTransitionError("fail", *item, err)
		}
	}
}

func logTransitionError(operation string, item domain.Item, err error) {
	slog.Error("batch_item_transition_failed",
		"operation", operation,
		"batch_id", item.BatchID,
		"item_id", item.ID,
		"attempt", item.Attempts,
		"error", err,
	)
}

func RetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	case 3:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

func Classify(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED", false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT", true
	}
	v := strings.ToLower(err.Error())
	switch {
	case strings.Contains(v, "access_denied") || strings.Contains(v, "access_error"):
		return "ACCESS_DENIED", false
	case strings.Contains(v, "401") || strings.Contains(v, "403") || strings.Contains(v, "credencial"):
		return "AUTH", false
	case strings.Contains(v, "não encontrado") || strings.Contains(v, "not found"):
		return "NOT_FOUND", false
	case strings.Contains(v, "429"):
		return "RATE_LIMIT", true
	case strings.Contains(v, "timeout") || strings.Contains(v, "connection") || strings.Contains(v, "conexão") || strings.Contains(v, "transport"):
		return "TRANSPORT", true
	case strings.Contains(v, "500") || strings.Contains(v, "502") || strings.Contains(v, "503") || strings.Contains(v, "ollama"):
		return "DEPENDENCY", true
	default:
		return "UNKNOWN", false
	}
}
