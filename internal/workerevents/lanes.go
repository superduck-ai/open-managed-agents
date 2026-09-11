package workerevents

import "strings"

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

func (lane deliveryLane) subject(codeSessionID string) (string, error) {
	subject, err := Subject(codeSessionID)
	if err != nil {
		return "", err
	}
	return subject + string(lane), nil
}

func (lane deliveryLane) consumerName(codeSessionID string) string {
	if lane == replyLane {
		return consumerName(codeSessionID) + "_reply"
	}
	return consumerName(codeSessionID)
}

func sessionFromSubject(subject string) (string, bool) {
	if !strings.HasPrefix(subject, subjectPrefix) {
		return "", false
	}
	sessionID := strings.TrimSuffix(strings.TrimPrefix(subject, subjectPrefix), string(replyLane))
	if _, err := Subject(sessionID); err != nil {
		return "", false
	}
	return sessionID, true
}
