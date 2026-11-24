package db

import "context"

const (
	HarnessChoiceResponse = "choice_response"
	HarnessToolResult     = "tool_result"
)

type HarnessWake struct {
	Turn      HarnessTurnRef
	InputUUID string
	Kind      string
}

func (d *DB) HarnessResumeWork(ctx context.Context, ref HarnessTurnRef, kind string) (HarnessTask, bool, error) {
	row, found, err := NewCloudHarnessMapper(d.mapperDB).FindResumeWork(ctx, ref, kind)
	return row.task(), found, err
}

func (d *DB) ListReadyHarnessInputs(ctx context.Context, limit int) ([]HarnessWake, error) {
	rows, err := NewCloudHarnessMapper(d.mapperDB).ListReadyInputs(ctx, limit)
	if err != nil {
		return nil, err
	}
	inputs := make([]HarnessWake, 0, len(rows))
	for _, row := range rows {
		inputs = append(inputs, HarnessWake{
			Turn:      HarnessTurnRef{UUID: row.UUID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, SessionUUID: row.SessionUUID},
			InputUUID: row.ResumeInputUUID, Kind: row.Kind,
		})
	}
	return inputs, nil
}

func (d *DB) MarkHarnessInputPublished(ctx context.Context, input HarnessWake) error {
	return NewCloudHarnessMapper(d.mapperDB).MarkInputPublished(ctx, input.Turn, input.InputUUID)
}
