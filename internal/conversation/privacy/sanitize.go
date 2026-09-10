package privacy

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	imageTag = regexp.MustCompile(`(?is)\[img\].*?\[/img\]`)
	bbCode   = regexp.MustCompile(`(?is)\[/?[a-z]+(?:=[^\]]+)?\]`)
	email    = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	url      = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s<>"']+|\bwww\.[^\s<>"']+`)
	bearer   = regexp.MustCompile(`(?i)\bBearer[ \t]+[A-Za-z0-9._~+/=-]+`)
	secret   = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|client[_-]?secret|webhook[_-]?token|token|password|secret)([ \t]*[:=][ \t]*)([^\s,;]+)`)
	jwt      = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	spaces   = regexp.MustCompile(`[ \t]+`)
	phone    = regexp.MustCompile(`\b(?:\+?55\s*)?(?:\(?\d{2}\)?\s*)?\d{4,5}[-.\s]?\d{4}\b`)
	document = regexp.MustCompile(`\b\d{3}\.?\d{3}\.?\d{3}-?\d{2}\b|\b\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}\b`)
	ip       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

// Sanitize removes credentials and online identifiers while retaining names
// and telephone numbers needed to understand a commercial conversation.
func Sanitize(raw string) string {
	text := imageTag.ReplaceAllString(raw, "")
	text = bbCode.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = email.ReplaceAllString(text, "[email]")
	text = bearer.ReplaceAllString(text, "Bearer [token]")
	text = secret.ReplaceAllString(text, "$1$2[token]")
	text = jwt.ReplaceAllString(text, "[token]")
	text = url.ReplaceAllString(text, "[url]")

	var clean []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(spaces.ReplaceAllString(line, " "))
		if line != "" {
			clean = append(clean, line)
		}
	}
	return strings.Join(clean, " ")
}

type Session struct {
	Mode    string
	aliases map[string]string
	counts  map[string]int
}

func NewSession(mode string) *Session {
	return &Session{Mode: mode, aliases: map[string]string{}, counts: map[string]int{}}
}
func (s *Session) RegisterName(name, role string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	prefix := "CLIENTE"
	if role == "ATENDENTE" {
		prefix = "ATENDENTE"
	}
	s.alias(name, prefix)
}
func (s *Session) Transform(raw string) string {
	text := stripMarkup(raw)
	text = bearer.ReplaceAllString(text, "Bearer [token]")
	text = secret.ReplaceAllString(text, "$1$2[token]")
	text = jwt.ReplaceAllString(text, "[token]")
	if s.Mode != "preserve" {
		for original, alias := range s.aliases {
			text = strings.ReplaceAll(text, original, alias)
		}
		text = replaceAliases(text, email, "EMAIL", s)
		text = replaceAliases(text, phone, "TELEFONE", s)
		text = replaceAliases(text, document, "DOCUMENTO", s)
		text = replaceAliases(text, ip, "IP", s)
		text = replaceAliases(text, url, "URL", s)
	} else {
		text = redactSecretURLs(text)
	}
	return clean(text)
}
func (s *Session) alias(value, prefix string) string {
	if a := s.aliases[value]; a != "" {
		return a
	}
	s.counts[prefix]++
	a := fmt.Sprintf("%s_%d", prefix, s.counts[prefix])
	s.aliases[value] = a
	return a
}
func replaceAliases(text string, re *regexp.Regexp, prefix string, s *Session) string {
	return re.ReplaceAllStringFunc(text, func(v string) string { return s.alias(v, prefix) })
}
func stripMarkup(raw string) string {
	text := imageTag.ReplaceAllString(raw, "")
	text = bbCode.ReplaceAllString(text, "")
	return html.UnescapeString(text)
}
func clean(text string) string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(spaces.ReplaceAllString(line, " "))
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, " ")
}
func redactSecretURLs(text string) string {
	return url.ReplaceAllStringFunc(text, func(v string) string {
		lower := strings.ToLower(v)
		if strings.Contains(lower, "token=") || strings.Contains(lower, "key=") || strings.Contains(lower, "secret=") || strings.Contains(lower, "/rest/") {
			return "[url redigida]"
		}
		return v
	})
}
