package workerevents

import "errors"

var (
	errAckSubjectEmpty       = errors.New("worker event ACK subject is empty")
	errMemoryAckNotFound     = errors.New("memory worker event ACK not found")
	errMemoryAckNotQueueHead = errors.New("memory worker event ACK is not queue head")
)
