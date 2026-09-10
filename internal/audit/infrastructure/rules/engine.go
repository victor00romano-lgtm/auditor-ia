package rules

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/audit/domain"
	"gopkg.in/yaml.v3"
)

type Condition struct {
	Field    string
	Operator string
	Value    string
}

type Rule struct {
	Name        string
	Description string
	Entity      string
	Field       string
	Operator    string
	Value       string
	Severity    domain.Severity
	Conditions  []Condition
}

type Engine struct {
	Rules []Rule
	Now   func() time.Time
}

func Load(path string) (*Engine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Rules []Rule `yaml:"rules"`
	}
	if err = yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &Engine{Rules: cfg.Rules, Now: time.Now}, nil
}

func (e *Engine) Evaluate(records []domain.Record) ([]domain.Finding, error) {
	var out []domain.Finding
	for _, record := range records {
		for _, rule := range e.Rules {
			if rule.Entity != record.Type {
				continue
			}
			matched, err := e.evaluateRule(record.Fields, rule)
			if err != nil {
				return nil, fmt.Errorf("regra %s: %w", rule.Name, err)
			}
			if matched {
				out = append(out, domain.Finding{Rule: rule.Name, Description: rule.Description, EntityType: record.Type, EntityID: record.ID, Severity: rule.Severity})
			}
		}
	}
	return out, nil
}

func (e *Engine) evaluateRule(
	fields map[string]any,
	rule Rule,
) (bool, error) {
	if len(rule.Conditions) == 0 {
		return e.matches(fields[rule.Field], rule)
	}

	for _, condition := range rule.Conditions {
		matched, err := e.matches(
			fields[condition.Field],
			Rule{
				Operator: condition.Operator,
				Value:    condition.Value,
			},
		)
		if err != nil {
			return false, err
		}

		if !matched {
			return false, nil
		}
	}

	return true, nil
}

func (e *Engine) matches(raw any, r Rule) (bool, error) {
	s := strings.TrimSpace(fmt.Sprint(raw))
	switch r.Operator {
	case "is_empty":
		return raw == nil || s == "" || s == "<nil>" || s == "0", nil
	case "not_empty":
		return raw != nil && s != "" && s != "<nil>", nil
	case "equals":
		return s == r.Value, nil
	case "greater_than", "less_than":
		a, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return false, nil
		}
		b, err := strconv.ParseFloat(r.Value, 64)
		if err != nil {
			return false, err
		}
		if r.Operator == "greater_than" {
			return a > b, nil
		}
		return a < b, nil
	case "older_than_days":
		days, err := strconv.Atoi(r.Value)
		if err != nil {
			return false, err
		}
		date, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return false, nil
		}
		return date.Before(e.Now().AddDate(0, 0, -days)), nil
	default:
		return false, fmt.Errorf("operador desconhecido: %s", r.Operator)
	}
}
