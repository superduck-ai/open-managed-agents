package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	yourbatis "github.com/superduck-ai/yourbatis"
)

func TestTableMappersBuildDynamicQueries(t *testing.T) {
	organizationUUID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspaceUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	codeSessionUUID := uuid.MustParse("33333333-3333-4333-8333-333333333333")

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

	t.Run("activation code session lock", func(t *testing.T) {
		bound := buildCodeSessionMapperLockInitializingCodeSession(
			yourbatis.DialectPostgres,
			workspaceUUID,
			codeSessionUUID,
		)
		assertMapperSQLContains(t, bound, "WHERE workspace_uuid = $1 AND uuid = $2")
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
}

func TestTableMappersBuildWrites(t *testing.T) {
	organizationUUID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspaceUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	codeSessionUUID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
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
}

func TestListExistingActivationInboundEventsBatchesLookup(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{})
	inputs := make([]AppendCodeSessionEventInput, managedAgentActivationInboundBatchSize+1)
	for index := range inputs {
		inputs[index].IdempotencyKey = fmt.Sprintf("idem-%d", index)
	}

	_, err := listExistingActivationInboundEvents(
		context.Background(),
		NewCodeSessionInboundEventMapper(executor),
		CodeSession{
			ExternalID:       "cse_test",
			OrganizationUUID: "11111111-1111-4111-8111-111111111111",
			WorkspaceUUID:    "22222222-2222-4222-8222-222222222222",
		},
		inputs,
	)
	if err != nil {
		t.Fatalf("list existing activation inbound events: %v", err)
	}
	if executor.queryCallCount != 2 {
		t.Fatalf("activation idempotency lookup calls = %d, want 2", executor.queryCallCount)
	}
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
