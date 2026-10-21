package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type sessionStore interface {
	GetCodeSessionBySessionExternalID(ctx context.Context, workspaceUUID, sessionExternalID string) (db.CodeSession, error)
	MergeCodeSessionWorkerExternalMetadata(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string, patch json.RawMessage) error
}

type reconcileRequest struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionExternalID string
	Acknowledge       []string
	Now               time.Time
}

type unknownOutcome struct {
	ToolUseID       string  `json:"tool_use_id"`
	ToolName        *string `json:"tool_name"`
	SessionThreadID string  `json:"session_thread_id,omitempty"`
}

type reconciliationRequest struct {
	ToolUseIDs  []string  `json:"tool_use_ids"`
	RequestedAt time.Time `json:"requested_at"`
}

type workerMetadata struct {
	UnknownOutcomes []unknownOutcome       `json:"unknown_tool_outcomes"`
	Reconciliation  *reconciliationRequest `json:"tool_outcome_reconciliation"`
}

type reconcileReport struct {
	CodeSessionID         string                 `json:"code_session_id"`
	UnknownOutcomes       []unknownOutcome       `json:"unknown_tool_outcomes"`
	PendingReconciliation *reconciliationRequest `json:"tool_outcome_reconciliation"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tool-outcome-reconcile:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	request, err := parseRequest(args, stderr)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(logging.NewConsoleHandler(stderr, slog.LevelInfo)).With("component", "tool-outcome-reconcile-cli")
	slog.SetDefault(logger)
	database, err := db.OpenExisting(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	defer database.Close()
	return reconcile(context.Background(), database, request, stdout)
}

func parseRequest(args []string, stderr io.Writer) (reconcileRequest, error) {
	flags := flag.NewFlagSet("tool-outcome-reconcile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	organization := flags.String("organization", "", "organization UUID")
	workspace := flags.String("workspace", "", "workspace UUID")
	session := flags.String("session", "", "public session ID")
	acknowledge := flags.String("acknowledge", "", "comma-separated tool_use_id values whose unknown outcomes are accepted without replay")
	if err := flags.Parse(args); err != nil {
		return reconcileRequest{}, err
	}
	request := reconcileRequest{
		OrganizationUUID:  strings.TrimSpace(*organization),
		WorkspaceUUID:     strings.TrimSpace(*workspace),
		SessionExternalID: strings.TrimSpace(*session),
		Now:               time.Now().UTC(),
	}
	for _, value := range []string{request.OrganizationUUID, request.WorkspaceUUID} {
		if _, err := uuid.Parse(value); err != nil {
			return reconcileRequest{}, errors.New("organization and workspace must be UUIDs")
		}
	}
	if request.SessionExternalID == "" {
		return reconcileRequest{}, errors.New("session is required")
	}
	for _, id := range strings.Split(*acknowledge, ",") {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(request.Acknowledge, id) {
			request.Acknowledge = append(request.Acknowledge, id)
		}
	}
	return request, nil
}

func reconcile(ctx context.Context, store sessionStore, request reconcileRequest, stdout io.Writer) error {
	session, err := store.GetCodeSessionBySessionExternalID(ctx, request.WorkspaceUUID, request.SessionExternalID)
	if err == nil && session.OrganizationUUID != request.OrganizationUUID {
		err = db.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find session: %w", err)
	}
	var metadata workerMetadata
	if len(session.WorkerExternalMetadata) > 0 {
		if err := json.Unmarshal(session.WorkerExternalMetadata, &metadata); err != nil {
			return fmt.Errorf("decode worker metadata: %w", err)
		}
	}
	if len(request.Acknowledge) == 0 {
		return writeReport(stdout, reconcileReport{CodeSessionID: session.ExternalID, UnknownOutcomes: metadata.UnknownOutcomes, PendingReconciliation: metadata.Reconciliation})
	}
	for _, id := range request.Acknowledge {
		if !slices.ContainsFunc(metadata.UnknownOutcomes, func(outcome unknownOutcome) bool { return outcome.ToolUseID == id }) {
			return fmt.Errorf("tool_use_id %q is not an unknown tool outcome of this session", id)
		}
	}
	pending := &reconciliationRequest{ToolUseIDs: request.Acknowledge, RequestedAt: request.Now}
	patch, err := json.Marshal(map[string]any{"tool_outcome_reconciliation": pending})
	if err != nil {
		return err
	}
	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, request.OrganizationUUID, request.WorkspaceUUID, session.ExternalID, patch); err != nil {
		return fmt.Errorf("record reconciliation: %w", err)
	}
	return writeReport(stdout, reconcileReport{CodeSessionID: session.ExternalID, UnknownOutcomes: metadata.UnknownOutcomes, PendingReconciliation: pending})
}

func writeReport(stdout io.Writer, report reconcileReport) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
