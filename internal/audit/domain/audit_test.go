package domain

import (
	"testing"
	"time"
)

func TestScoreRejectsInvalidValue(t *testing.T) {
	if _, err := NewScore(101); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestFinishCalculatesScore(t *testing.T) {
	a, _ := Start("1", "company", time.Now())
	a.Finish([]Finding{{Severity: SeverityHigh}, {Severity: SeverityMedium}}, "", time.Now())
	if a.Score.Value() != 70 {
		t.Fatalf("score = %d, esperado 70", a.Score.Value())
	}
}
