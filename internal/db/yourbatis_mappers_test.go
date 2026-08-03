package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	yourbatis "github.com/superduck-ai/yourbatis"
)

func TestTableMappersRejectInvalidUUIDBeforeExecution(t *testing.T) {
	err := (ManagedAgentActivationTx{}).DeleteSessionEventQueue(context.Background(), "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "session_uuid must be a non-nil UUID") {
		t.Fatalf("DeleteSessionEventQueue() error = %v", err)
	}
}

func TestTableMappersBuildDynamicQueries(t *testing.T) {
	organizationUUID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspaceUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	sessionUUID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	eventUUIDOne := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	eventUUIDTwo := uuid.MustParse("55555555-5555-4555-8555-555555555555")

	t.Run("single code session event append", func(t *testing.T) {
		lockBound := buildCodeSessionMapperLockCodeSessionByExternalID(
			yourbatis.DialectPostgres,
			"cse_test",
		)
		assertMapperSQLContains(t, lockBound, "WHERE external_id = $1 AND deleted_at IS NULL FOR UPDATE")
		if strings.Contains(lockBound.SQL, "status = 'initializing'") {
			t.Fatalf("daily append lock unexpectedly restricts status: %s", lockBound.SQL)
		}

		inboundBound := buildCodeSessionInboundEventMapperGetCodeSessionInboundEventByIdempotencyKey(
			yourbatis.DialectPostgres,
			workspaceUUID,
			"idem-inbound",
		)
		assertMapperSQLContains(t, inboundBound, "workspace_uuid = $1 AND idempotency_key = $2 AND deleted_at IS NULL")

		outboundBound := buildCodeSessionOutboundEventMapperGetCodeSessionOutboundEventByIdempotencyKey(
			yourbatis.DialectPostgres,
			workspaceUUID,
			"idem-outbound",
		)
		assertMapperSQLContains(t, outboundBound, "workspace_uuid = $1 AND idempotency_key = $2 AND deleted_at IS NULL")
	})

	t.Run("idempotency keys", func(t *testing.T) {
		bound := buildCodeSessionInboundEventMapperListExistingActivationInboundEvents(
			yourbatis.DialectPostgres,
			organizationUUID,
			workspaceUUID,
			[]string{"idem-one", "idem-two"},
		)
		assertMapperSQLContains(t, bound, "idempotency_key IN ( $3 , $4 )")
		assertMapperArgumentNames(t, bound, []string{
			"organizationUUID",
			"workspaceUUID",
			"idempotencyKey",
			"idempotencyKey",
		})
	})

	t.Run("event UUIDs", func(t *testing.T) {
		bound := buildSessionEventMapperListSessionEventsByUUIDs(
			yourbatis.DialectPostgres,
			sessionUUID,
			[]uuid.UUID{eventUUIDOne, eventUUIDTwo},
		)
		assertMapperSQLContains(t, bound, "uuid IN ( $1 , $2 )")
		assertMapperSQLContains(t, bound, "session_uuid = $3")
		assertMapperArgumentNames(t, bound, []string{
			"sessionEventUUID",
			"sessionEventUUID",
			"sessionUUID",
		})
	})

	t.Run("queue batch", func(t *testing.T) {
		bound := buildSessionEventQueueMapperEnqueueSessionEvents(
			yourbatis.DialectPostgres,
			[]sessionEventQueueInsertRow{
				{
					OrganizationUUID: organizationUUID,
					WorkspaceUUID:    workspaceUUID,
					SessionUUID:      sessionUUID,
					SessionEventUUID: eventUUIDOne,
				},
				{
					OrganizationUUID: organizationUUID,
					WorkspaceUUID:    workspaceUUID,
					SessionUUID:      sessionUUID,
					SessionEventUUID: eventUUIDTwo,
				},
			},
		)
		assertMapperSQLContains(t, bound, "( $1, $2, $3, $4 ) , ( $5, $6, $7, $8 )")
		if len(bound.Args) != 8 {
			t.Fatalf("queue batch argument count = %d, want 8", len(bound.Args))
		}
	})

	t.Run("queue exists", func(t *testing.T) {
		bound := buildSessionEventQueueMapperSessionEventQueueExists(
			yourbatis.DialectPostgres,
			sessionUUID,
		)
		assertMapperSQLEquals(
			t,
			bound,
			"SELECT EXISTS ( SELECT 1 FROM session_event_queue WHERE session_uuid = $1 )",
		)
		assertMapperArgumentNames(t, bound, []string{"sessionUUID"})
	})

	t.Run("queue identities", func(t *testing.T) {
		bound := buildSessionEventQueueMapperListSessionEventQueueIdentities(
			yourbatis.DialectPostgres,
			sessionUUID,
		)
		assertMapperSQLEquals(
			t,
			bound,
			"SELECT id, session_event_uuid FROM session_event_queue WHERE session_uuid = $1 ORDER BY id ASC FOR UPDATE",
		)
		assertMapperArgumentNames(t, bound, []string{"sessionUUID"})
	})
}

func TestTableMappersBuildWrites(t *testing.T) {
	organizationUUID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspaceUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	codeSessionUUID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	sessionUUID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	threadUUID := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	eventUUID := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	createdAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	row := codeSessionInboundEventInsertRow{
		ExternalID:            "evt_test",
		OrganizationUUID:      organizationUUID,
		WorkspaceUUID:         workspaceUUID,
		CodeSessionUUID:       codeSessionUUID,
		CodeSessionExternalID: "codeses_test",
		SequenceNum:           1,
		EventType:             "user",
		EventSubtype:          "message",
		Payload:               []byte(`{"type":"user"}`),
		PayloadHash:           "hash",
		IdempotencyKey:        "idem",
		DeliveryStatus:        "queued",
		Source:                "session_event",
		CreatedAt:             createdAt,
	}
	bound := buildCodeSessionInboundEventMapperInsertCodeSessionInboundEvents(
		yourbatis.DialectPostgres,
		[]codeSessionInboundEventInsertRow{row, row},
	)
	assertMapperSQLContains(t, bound, "CAST($11 AS jsonb)")
	assertMapperSQLContains(t, bound, "CAST($28 AS jsonb)")
	if len(bound.Args) != 34 {
		t.Fatalf("inbound batch argument count = %d, want 34", len(bound.Args))
	}

	singleInboundBound := buildCodeSessionInboundEventMapperInsertCodeSessionInboundEvent(
		yourbatis.DialectPostgres,
		row,
	)
	assertMapperSQLContains(t, singleInboundBound, "INSERT INTO code_session_inbound_events")
	assertMapperSQLContains(t, singleInboundBound, "RETURNING uuid, external_id")

	outboundRow := codeSessionOutboundEventInsertRow{
		ExternalID:            "evt_outbound_test",
		OrganizationUUID:      organizationUUID,
		WorkspaceUUID:         workspaceUUID,
		CodeSessionUUID:       codeSessionUUID,
		CodeSessionExternalID: "codeses_test",
		SequenceNum:           1,
		EventType:             "assistant",
		EventSubtype:          "message",
		Payload:               []byte(`{"type":"assistant"}`),
		PayloadHash:           "hash-outbound",
		IdempotencyKey:        "idem-outbound",
		Source:                "worker",
		Ephemeral:             true,
		CreatedAt:             createdAt,
	}
	singleOutboundBound := buildCodeSessionOutboundEventMapperInsertCodeSessionOutboundEvent(
		yourbatis.DialectPostgres,
		outboundRow,
	)
	assertMapperSQLContains(t, singleOutboundBound, "INSERT INTO code_session_outbound_events")
	assertMapperSQLContains(t, singleOutboundBound, "RETURNING uuid, external_id")

	outcomeBound := buildSessionMapperSetSessionOutcomeEvaluations(
		yourbatis.DialectPostgres,
		workspaceUUID,
		"sesn_test",
		[]byte(`[{"score":1}]`),
	)
	assertMapperSQLContains(t, outcomeBound, "outcome_evaluations = CAST($1 AS jsonb)")
	assertMapperSQLContains(t, outcomeBound, "workspace_uuid = $2")
	assertMapperArgumentNames(t, outcomeBound, []string{
		"outcomeEvaluations",
		"workspaceUUID",
		"sessionExternalID",
	})

	threadBound := buildSessionThreadMapperGetSessionThreadByExternalID(
		yourbatis.DialectPostgres,
		workspaceUUID,
		"sesn_test",
		"sesthr_test",
	)
	assertMapperSQLContains(t, threadBound, "external_id = $3")
	assertMapperArgumentNames(t, threadBound, []string{
		"workspaceUUID",
		"sessionExternalID",
		"threadExternalID",
	})

	eventBound := buildSessionEventMapperInsertSessionEvent(
		yourbatis.DialectPostgres,
		sessionEventInsertRow{
			UUID:              eventUUID,
			ExternalID:        "event_test",
			OrganizationUUID:  organizationUUID,
			WorkspaceUUID:     workspaceUUID,
			SessionUUID:       sessionUUID,
			SessionExternalID: "sesn_test",
			ThreadUUID:        threadUUID,
			ThreadExternalID:  "sesthr_test",
			EventType:         "user.message",
			Payload:           []byte(`{"type":"user.message"}`),
			ProcessedAt:       createdAt,
			CreatedAt:         createdAt,
		},
	)
	assertMapperSQLContains(t, eventBound, "CAST($10 AS jsonb)")
	assertMapperArgumentNames(t, eventBound, []string{
		"row.UUID",
		"row.ExternalID",
		"row.OrganizationUUID",
		"row.WorkspaceUUID",
		"row.SessionUUID",
		"row.SessionExternalID",
		"row.ThreadUUID",
		"row.ThreadExternalID",
		"row.EventType",
		"row.Payload",
		"row.ProcessedAt",
		"row.CreatedAt",
	})
}

func assertMapperSQLContains(
	t *testing.T,
	bound yourbatis.BoundSQL,
	want string,
) {
	t.Helper()
	compact := strings.Join(strings.Fields(bound.SQL), " ")
	if !strings.Contains(compact, want) {
		t.Fatalf("SQL does not contain %q:\n%s", want, compact)
	}
}

func assertMapperSQLEquals(t *testing.T, bound yourbatis.BoundSQL, want string) {
	t.Helper()
	got := strings.Join(strings.Fields(bound.SQL), " ")
	if got != want {
		t.Fatalf("SQL = %q, want %q", got, want)
	}
}

func assertMapperArgumentNames(
	t *testing.T,
	bound yourbatis.BoundSQL,
	want []string,
) {
	t.Helper()
	got := make([]string, len(bound.Args))
	for index, argument := range bound.Args {
		got[index] = argument.Name
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("argument names = %v, want %v", got, want)
	}
}
