package application

import (
	"strconv"

	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

// DealRecord adapts the persisted Bitrix deal to the generic rule-engine input.
func DealRecord(deal dealdomain.Deal) auditdomain.Record {
	fields := map[string]any{
		"stage_id":          pointerString(deal.StageID),
		"stage_semantic_id": pointerString(deal.StageSemanticID),
	}
	if deal.Closed != nil {
		if *deal.Closed {
			fields["closed"] = "Y"
		} else {
			fields["closed"] = "N"
		}
	}
	if deal.Amount != nil {
		fields["amount"] = *deal.Amount
		fields["opportunity"] = *deal.Amount
	}
	if deal.AssignedByID != nil {
		fields["owner_id"] = *deal.AssignedByID
		fields["assigned_by_id"] = *deal.AssignedByID
	}
	if deal.UpdatedAtBitrix != nil {
		fields["updated_at"] = deal.UpdatedAtBitrix.Format("2006-01-02T15:04:05Z07:00")
		fields["date_modify"] = fields["updated_at"]
	}
	return auditdomain.Record{Type: "deal", ID: strconv.FormatInt(deal.BitrixDealID, 10), Fields: fields}
}

func pointerString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
