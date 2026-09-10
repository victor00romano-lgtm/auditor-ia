package application

import "context"

// ConfiguredAssigneeRoles classifies responsible users by their stable Bitrix
// IDs. Names may still be displayed as evidence, but are not classification keys.
type ConfiguredAssigneeRoles struct {
	support map[int64]struct{}
}

func NewConfiguredAssigneeRoles(supportIDs []int64) ConfiguredAssigneeRoles {
	roles := ConfiguredAssigneeRoles{support: make(map[int64]struct{}, len(supportIDs))}
	for _, id := range supportIDs {
		if id > 0 {
			roles.support[id] = struct{}{}
		}
	}
	return roles
}

func (roles ConfiguredAssigneeRoles) IsSupport(_ context.Context, assigneeID int64) (bool, error) {
	_, ok := roles.support[assigneeID]
	return ok, nil
}
