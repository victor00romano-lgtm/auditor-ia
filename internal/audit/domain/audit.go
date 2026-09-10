package domain

import (
	"errors"
	"fmt"
	"time"
)

type Status string

const (
	StatusRunning  Status = "RUNNING"
	StatusFinished Status = "FINISHED"
	StatusFailed   Status = "FAILED"
)

type Severity string

const (
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

type Score struct{ value int }

func NewScore(value int) (Score, error) {
	if value < 0 || value > 100 {
		return Score{}, fmt.Errorf("score deve estar entre 0 e 100: %d", value)
	}
	return Score{value: value}, nil
}

func (s Score) Value() int { return s.value }

type Finding struct {
	Rule, Description, EntityType, EntityID string
	Severity                                Severity
}

type Audit struct {
	ID, CompanyID    string
	Status           Status
	Score            Score
	Findings         []Finding
	AIAnalysis       string
	StartedAt        time.Time
	FinishedAt       *time.Time
	ProcessedRecords int
}

func Start(id, companyID string, now time.Time) (*Audit, error) {
	if id == "" || companyID == "" {
		return nil, errors.New("id e companyID são obrigatórios")
	}
	score, _ := NewScore(100)
	return &Audit{ID: id, CompanyID: companyID, Status: StatusRunning, Score: score, StartedAt: now}, nil
}

func (a *Audit) Finish(findings []Finding, analysis string, now time.Time) {
	a.Score = CalculateScore(findings)
	a.Findings, a.AIAnalysis, a.Status, a.FinishedAt = findings, analysis, StatusFinished, &now
}

// CalculateScore applies the deterministic penalty policy used by audits.
func CalculateScore(findings []Finding) Score {
	penalty := 0
	for _, f := range findings {
		switch f.Severity {
		case SeverityHigh, SeverityCritical:
			penalty += 20
		case SeverityMedium:
			penalty += 10
		default:
			penalty += 5
		}
	}
	if penalty > 100 {
		penalty = 100
	}
	score, _ := NewScore(100 - penalty)
	return score
}
