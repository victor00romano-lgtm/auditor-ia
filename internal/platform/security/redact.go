package security

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	credentialURL = regexp.MustCompile(`(?i)\b(https?|postgres(?:ql)?)://([^\s/@]+)(?::([^\s/@]*))?@`)
	bitrixURL     = regexp.MustCompile(`(?i)(https?://[^\s/]+/rest/)[^/\s]+/[^/\s]+`)
	bearer        = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	jwt           = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	parameter     = regexp.MustCompile(`(?i)(\b(?:token|key|api_key|api-key|password|passwd|secret)\b\s*[=:]\s*)([^\s&;,]+)`)
)

// Text returns a presentation-safe copy. It never mutates or wraps the
// original error, so callers may continue using errors.Is/errors.As internally.
func Text(value string) string {
	for _, name := range []string{"BITRIX_WEBHOOK_URL", "DATABASE_URL", "DATABASE_MIGRATOR_URL", "AUDITOR_API_KEY"} {
		if secret := strings.TrimSpace(os.Getenv(name)); secret != "" {
			value = strings.ReplaceAll(value, secret, "[redigido]")
		}
	}
	value = bitrixURL.ReplaceAllString(value, `${1}***/***`)
	value = credentialURL.ReplaceAllString(value, `${1}://***:***@`)
	value = bearer.ReplaceAllString(value, "Bearer [redigido]")
	value = jwt.ReplaceAllString(value, "[jwt redigido]")
	return parameter.ReplaceAllString(value, `${1}[redigido]`)
}

func Error(err error) string {
	if err == nil {
		return ""
	}
	return Text(err.Error())
}

func URL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return Text(raw)
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("***", "***")
	}
	return Text(parsed.String())
}

type Logger interface{ Printf(string, ...any) }

type RedactingLogger struct{ Next Logger }

func (l RedactingLogger) Printf(format string, args ...any) {
	if l.Next != nil {
		l.Next.Printf("%s", Text(fmt.Sprintf(format, args...)))
	}
}
