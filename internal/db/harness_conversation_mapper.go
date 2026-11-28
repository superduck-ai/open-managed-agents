package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper HarnessConversationMapper -sql ./harness_conversation_mapper.xml -out ./harness_conversation_mapper.sqlmap.gen.go -dialect postgres

type harnessConversationScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	SessionUUID      string
}

type harnessThreadKey struct {
	Scope harnessConversationScope
	UUID  string
}

type harnessConversationThread struct {
	ThreadUUID       string  `db:"thread_uuid"`
	RootThreadUUID   string  `db:"root_thread_uuid"`
	ParentThreadUUID *string `db:"parent_thread_uuid"`
	SpawnMessageSeq  *int64  `db:"spawn_message_seq"`
	SpawnToolCallID  *string `db:"spawn_tool_call_id"`
	Closed           bool    `db:"closed"`
	Compaction       bool    `db:"is_compaction"`
	Fresh            bool    `db:"fresh_context"`
	MessageCount     int64   `db:"message_count"`
}

type harnessSummaryRow struct {
	Summary string `db:"summary"`
	UpToSeq int64  `db:"up_to_seq"`
}

type HarnessConversationMapper interface {
	EnsureRoot(ctx context.Context, key harnessThreadKey) (int64, error)
	FindThread(ctx context.Context, key harnessThreadKey) (harnessConversationThread, bool, error)
	LockThread(ctx context.Context, key harnessThreadKey) (harnessConversationThread, error)
	InsertThread(ctx context.Context, key harnessThreadKey, thread harnessConversationThread) error
	CloseThread(ctx context.Context, key harnessThreadKey) error
	MarkCompaction(ctx context.Context, key harnessThreadKey) error
	LoadMessages(ctx context.Context, key harnessThreadKey, startFrom, upTo *int64) ([]string, error)
	HasMessageID(ctx context.Context, key harnessThreadKey, messageID string) (bool, error)
	ListTurnMessages(ctx context.Context, key harnessThreadKey, turnUUID string) ([]string, error)
	AppendMessages(ctx context.Context, key harnessThreadKey, turnUUID *string, messageID, data string, messageSeq int64) (int64, error)
	UpdateMessageCount(ctx context.Context, key harnessThreadKey, messageCount int64) error
	DiscardTurn(ctx context.Context, key harnessThreadKey, turnUUID string) (int64, error)
	SaveSummary(ctx context.Context, key harnessThreadKey, summary string, upToSeq int64) error
	FindSummary(ctx context.Context, key harnessThreadKey, upToSeq *int64) (harnessSummaryRow, bool, error)
	DeleteHistory(ctx context.Context, scope harnessConversationScope) error
	DeleteSummaries(ctx context.Context, scope harnessConversationScope) error
	DeleteThreads(ctx context.Context, scope harnessConversationScope) error
}
