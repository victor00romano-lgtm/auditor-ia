package httpapi

import (
	"testing"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

func TestAnalysisDTOExposesDeterministicFinalResult(t *testing.T) {
	for _, input := range []apidomain.Analysis{
		{BitrixDealID: 34710, ProbableResult: "indeterminado", FinalResult: "pós-venda/suporte", ResultSource: "responsável de suporte no Bitrix"},
		{BitrixDealID: 34720, ProbableResult: "em negociação", FinalResult: "não venda", ResultSource: "estágio perdido no Bitrix"},
		{BitrixDealID: 34734, ProbableResult: "venda", FinalResult: "não venda", ResultSource: "estágio perdido no Bitrix"},
	} {
		got := toAnalysisDTO(input, false)
		if got.FinalResult != input.FinalResult || got.ResultSource != input.ResultSource || got.ProbableResult != input.ProbableResult {
			t.Fatalf("negócio %d DTO=%#v", input.BitrixDealID, got)
		}
	}
}
