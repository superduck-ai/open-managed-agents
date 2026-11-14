package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionInputRequestMapper -sql ./session_input_request_mapper.xml -out ./session_input_request_mapper.sqlmap.gen.go -dialect postgres

type sessionInputRequestRow struct {
	Fingerprint          string `db:"fingerprint"`
	EventIDs             string `db:"event_ids"`
	SettledInputEventIDs string `db:"settled_input_event_ids"`
	HandoffCompleted     bool   `db:"handoff_completed"`
}

type SessionInputRequestMapper interface {
	Find(ctx context.Context, session Session, request SessionInputRequest) (sessionInputRequestRow, bool, error)
	Insert(ctx context.Context, session Session, request SessionInputRequest, row sessionInputRequestRow) error
	CompleteHandoff(ctx context.Context, session Session, request SessionInputRequest) (int64, error)
}
