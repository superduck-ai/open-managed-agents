package deploymentjobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
)

const Queue = "deployment_schedules"

type Args struct {
	WorkspaceUUID        string    `json:"workspace_uuid"`
	DeploymentExternalID string    `json:"deployment_id"`
	Schedule             Schedule  `json:"schedule"`
	ScheduledAt          time.Time `json:"scheduled_at"`
}

func (Args) Kind() string { return "scheduled_deployment" }

func (Args) Hooks() []rivertype.Hook {
	return []rivertype.Hook{river.HookInsertBeginFunc(stampScheduledDeploymentOccurrence)}
}

var _ river.JobArgsWithHooks = Args{}

// River overwrites river_job.scheduled_at on retry, so the Cron occurrence is stored in job args.
func stampScheduledDeploymentOccurrence(_ context.Context, params *rivertype.JobInsertParams) error {
	if params.ScheduledAt == nil {
		return errors.New("scheduled_deployment job requires scheduled_at")
	}
	args, err := jsonx.Decode[Args](json.RawMessage(params.EncodedArgs))
	if err != nil {
		return err
	}
	args.ScheduledAt = params.ScheduledAt.UTC()
	encoded, err := jsonx.Encode(args)
	if err != nil {
		return err
	}
	params.EncodedArgs = encoded
	params.Args = args
	return nil
}

func UpsertOpts(workspaceUUID, externalID string, schedule json.RawMessage) (*river.DurablePeriodicJobUpsertOpts, error) {
	parsed, err := Parse(schedule)
	if err != nil {
		return nil, err
	}
	args, err := jsonx.Encode(Args{
		WorkspaceUUID:        workspaceUUID,
		DeploymentExternalID: externalID,
		Schedule:             parsed.Config,
	})
	if err != nil {
		return nil, err
	}
	return &river.DurablePeriodicJobUpsertOpts{
		Args:  args,
		ID:    externalID,
		Kind:  Args{}.Kind(),
		Queue: Queue,
		Schedule: &river.DurablePeriodicJobSchedule{
			CronExpression: mapSundaySeven(parsed.Config.Expression),
			CronTimezone:   parsed.Config.Timezone,
		},
	}, nil
}
