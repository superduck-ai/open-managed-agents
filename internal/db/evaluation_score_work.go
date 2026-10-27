package db

import "context"

type EvaluationScoreWork struct {
	OrganizationUUID     string
	WorkspaceUUID        string
	ExperimentExternalID string
	ItemExternalID       string
	SessionExternalID    string
}

func (d *DB) ListEvaluationItemsMissingScores(ctx context.Context, limit int) ([]EvaluationScoreWork, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := NewEvaluationScoreWorkMapper(d.mapperDB).ListItemsMissingScores(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]EvaluationScoreWork, 0, len(rows))
	for _, row := range rows {
		items = append(items, EvaluationScoreWork{
			OrganizationUUID:     row.OrganizationUUID,
			WorkspaceUUID:        row.WorkspaceUUID,
			ExperimentExternalID: row.ExperimentExternalID,
			ItemExternalID:       row.ExternalID,
			SessionExternalID:    stringFromNullable(row.SessionExternalID),
		})
	}
	return items, nil
}

func (d *DB) FindEvaluationAgentScoreSession(ctx context.Context, organizationUUID, workspaceUUID, experimentID, itemID, versionID string) (bool, error) {
	rows, err := NewEvaluationScoreWorkMapper(d.mapperDB).FindAgentScoreSession(ctx, organizationUUID, workspaceUUID, experimentID, itemID, versionID)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}
