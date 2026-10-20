package workerevents

import (
	"errors"
	"strings"
)

const (
	// StreamName 是未配置 namespace 时的共享 worker 入站事件 Stream 名称。
	StreamName = "OMA_WORKER_INBOUND"
	// subjectPrefix 是未配置 namespace 时的会话级 subject 前缀。
	subjectPrefix = "oma.worker.inbound.v2."
	// brainWorkSubjectBase 是未配置 namespace 时的 Brain 唤醒 subject。
	brainWorkSubjectBase = "oma.brain.work.available"
	consumerPrefixBase   = "oma_worker_"
)

type naming struct {
	streamName       string
	subjectPrefix    string
	brainWorkSubject string
	consumerPrefix   string
}

func newNaming(namespace string) naming {
	if namespace == "" {
		return naming{
			streamName:       StreamName,
			subjectPrefix:    subjectPrefix,
			brainWorkSubject: brainWorkSubjectBase,
			consumerPrefix:   consumerPrefixBase,
		}
	}
	upper := strings.ToUpper(namespace)
	return naming{
		streamName:       "OMA_" + upper + "_WORKER_INBOUND",
		subjectPrefix:    "oma." + namespace + ".worker.inbound.v2.",
		brainWorkSubject: "oma." + namespace + ".brain.work.available",
		consumerPrefix:   "oma_" + namespace + "_worker_",
	}
}

func (n naming) streamSubject() string { return n.subjectPrefix + ">" }

func (n naming) subject(codeSessionID string) (string, error) {
	if codeSessionID == "" || strings.ContainsAny(codeSessionID, ".*> \t\r\n") {
		return "", errors.New("invalid code session ID for worker event subject")
	}
	return n.subjectPrefix + codeSessionID, nil
}

func (n naming) consumerName(codeSessionID string) string { return n.consumerPrefix + codeSessionID }

func (n naming) sessionFromSubject(subject string) (string, bool) {
	if !strings.HasPrefix(subject, n.subjectPrefix) {
		return "", false
	}
	sessionID := strings.TrimSuffix(strings.TrimPrefix(subject, n.subjectPrefix), string(replyLane))
	if _, err := n.subject(sessionID); err != nil {
		return "", false
	}
	return sessionID, true
}
