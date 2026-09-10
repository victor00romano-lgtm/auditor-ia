package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/observability"
	"github.com/portfolio/auditor-ia/internal/platform/security"
	"go.opentelemetry.io/otel/attribute"
)

var ErrAccessDenied = errors.New("Bitrix negou acesso")

type APIError struct {
	Operation  string
	Code       string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Bitrix %s: HTTP %d (%s)", e.Operation, e.HTTPStatus, e.Code)
}

func (e *APIError) Is(target error) bool {
	return target == ErrAccessDenied && (e.Code == "ACCESS_DENIED" || e.Code == "ACCESS_ERROR")
}

type Client struct {
	Webhook string
	HTTP    *http.Client
}

func (c Client) Call(ctx context.Context, method string, payload any, target any) (returnErr error) {
	started := time.Now()
	spanName := "bitrix.request"
	switch strings.TrimSuffix(method, ".json") {
	case "crm.deal.get":
		spanName = "bitrix.deal.get"
	case "crm.activity.list":
		spanName = "bitrix.forms.collect"
	case "imopenlines.session.history.get", "imopenlines.crm.chat.get":
		spanName = "bitrix.messages.collect"
	}
	ctx, span := observability.Tracer().Start(ctx, spanName)
	span.SetAttributes(attribute.String("dependency", "bitrix"), attribute.String("operation", strings.TrimSuffix(method, ".json")))
	defer func() {
		span.End()
		if metrics := observability.CurrentMetrics(); metrics != nil {
			status := map[bool]string{true: "error", false: "success"}[returnErr != nil]
			operation := strings.TrimSuffix(method, ".json")
			metrics.External.WithLabelValues("bitrix", operation, status).Inc()
			metrics.ExternalDuration.WithLabelValues("bitrix", operation, status).Observe(time.Since(started).Seconds())
			metrics.BitrixDuration.WithLabelValues(operation, status).Observe(time.Since(started).Seconds())
		}
	}()
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(c.Webhook, "/") + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return safeError("criar requisição Bitrix", err, c.Webhook)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return safeError("executar requisição Bitrix", err, c.Webhook)
	}
	defer resp.Body.Close()

	var envelope json.RawMessage
	var apiResponse struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decodificar resposta HTTP %d: %w", resp.StatusCode, err)
	}
	if err := json.Unmarshal(envelope, &apiResponse); err != nil {
		return err
	}
	if resp.StatusCode >= 300 || apiResponse.Error != "" {
		code := strings.ToUpper(strings.TrimSpace(apiResponse.Error))
		if code == "" {
			code = http.StatusText(resp.StatusCode)
		}
		return &APIError{Operation: strings.TrimSuffix(method, ".json"), Code: code, HTTPStatus: resp.StatusCode}
	}
	if err := json.Unmarshal(envelope, target); err != nil {
		return fmt.Errorf("decodificar resposta de %s: %w", strings.TrimSuffix(method, ".json"), err)
	}
	return nil
}

func safeError(operation string, err error, webhook string) error {
	message := strings.ReplaceAll(err.Error(), strings.TrimRight(webhook, "/"), "[webhook redigido]")
	return fmt.Errorf("%s: %s", operation, security.Text(message))
}
