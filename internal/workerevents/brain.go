package workerevents

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type BrainTask struct {
	SessionID    string `json:"session_id"`
	WorkerEpoch  int64  `json:"worker_epoch"`
	IngressToken string `json:"ingress_token"`
	ModelToken   string `json:"model_token"`
}

func (b *JetStreamBroker) PublishBrainTask(ctx context.Context, task BrainTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	taskID := task.SessionID + ":" + strconv.FormatInt(task.WorkerEpoch, 10)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = b.js.Publish(ctx, "oma.brain.tasks.v1", payload, jetstream.WithMsgID(taskID))
	return err
}
