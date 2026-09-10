package domain

import "context"

type Record struct {
	Type, ID string
	Fields   map[string]any
}

type Repository interface {
	Save(context.Context, *Audit) error
	Find(context.Context, string) (*Audit, error)
	List(context.Context) ([]*Audit, error)
}

type Source interface {
	Records(context.Context) ([]Record, error)
}
type RuleEvaluator interface {
	Evaluate([]Record) ([]Finding, error)
}
type FindingRepository interface {
	Save(context.Context, int64, int64, []Finding) (int, error)
}
type AIAnalyzer interface {
	Analyze(context.Context, *Audit) (string, error)
}
