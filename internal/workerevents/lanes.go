package workerevents

type deliveryLane string

const (
	taskLane  deliveryLane = ""
	replyLane deliveryLane = ".reply"
)

var deliveryLanes = [...]deliveryLane{taskLane, replyLane}

// laneForEvent only bypasses task ordering for messages that can unblock an
// executing command. Initialization and unknown requests retain task ordering.
func laneForEvent(envelope EnvelopeV1) deliveryLane {
	switch envelope.EventType {
	case "control_response":
		return replyLane
	case "control_request":
		if envelope.EventSubtype == "interrupt" {
			return replyLane
		}
	}
	return taskLane
}

func (lane deliveryLane) subject(names naming, codeSessionID string) (string, error) {
	subject, err := names.subject(codeSessionID)
	if err != nil {
		return "", err
	}
	return subject + string(lane), nil
}

func (lane deliveryLane) consumerName(names naming, codeSessionID string) string {
	if lane == replyLane {
		return names.consumerName(codeSessionID) + "_reply"
	}
	return names.consumerName(codeSessionID)
}
