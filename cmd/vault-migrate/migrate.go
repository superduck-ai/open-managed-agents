package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type migrationCounts struct{ success, skipped, failed int }

func (c *migrationCounts) add(other migrationCounts) {
	c.success += other.success
	c.skipped += other.skipped
	c.failed += other.failed
}

func printMigrationCounts(out io.Writer, label string, count migrationCounts) {
	fmt.Fprintf(out, "%-28s %10d %10d %10d\n", label, count.success, count.skipped, count.failed)
}

func printFailureReasons(out io.Writer, reasons map[string]int) {
	fmt.Fprintln(out, "\nSkipped: other providers or empty data.")
	if len(reasons) == 0 {
		return
	}
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Fprintf(out, "\nFailure reasons:\n%5s  %-28s %s\n", "Count", "Storage", "Reason")
	for _, key := range keys {
		fmt.Fprintf(out, "%5d  %s\n", reasons[key], key)
	}
}

func migrate(ctx context.Context, store *db.DB, service *secrets.Service, from, to string, apply bool, out io.Writer) error {
	reasons := make(map[string]int)
	var failedRecords []string
	defer func() {
		printFailureReasons(out, reasons)
		if len(failedRecords) > 0 {
			fmt.Fprintln(out, "\nFailed records:")
			for _, record := range failedRecords {
				fmt.Fprintln(out, record)
			}
		}
	}()
	reportFailure := func(kind, id string, failure error) {
		reasons[fmt.Sprintf("%-28s %s", kind, failure)]++
		failedRecords = append(failedRecords, fmt.Sprintf("FAILED %s %s: %v", kind, id, failure))
	}
	mode, success := "Preview", "Verified"
	if apply {
		mode, success = "Apply", "Migrated"
	}
	fmt.Fprintf(out, "%s: %s -> %s\n", mode, from, to)
	if !apply {
		fmt.Fprintln(out, "Read-only: verifies source DEKs; no target calls or database changes.")
	}
	fmt.Fprintln(out, "Counts are database records; each Deployment is processed as one record.")
	fmt.Fprintf(out, "\n%-28s %10s %10s %10s\n", "Storage", success, "Skipped", "Failed")
	total := migrationCounts{}
	for _, kind := range db.SecretMigrationKinds() {
		count, err := migrateKind(ctx, store, service, kind, from, apply, reportFailure)
		printMigrationCounts(out, kind, count)
		total.add(count)
		if err != nil {
			printMigrationCounts(out, "Total (partial)", total)
			return fmt.Errorf("%s stopped: %w", mode, err)
		}
	}
	printMigrationCounts(out, "Total", total)
	if total.failed > 0 {
		return fmt.Errorf("%d records failed; see reported reasons; failed records were not changed", total.failed)
	}
	if total.success == 0 {
		fmt.Fprintln(out, "\nNothing to migrate.")
	} else if apply {
		fmt.Fprintln(out, "\nMigration complete. Keep old keys while database backups still need them.")
	} else {
		fmt.Fprintln(out, "\nSource DEKs verified. Target access and business ciphertext were not checked.")
		fmt.Fprintln(out, "Next: back up the database and stop OMA/workers, then use the same CONFIG_FILE:")
		fmt.Fprintf(out, "  go run ./cmd/vault-migrate --from %s --to %s --apply\n", from, to)
	}
	return nil
}

func migrateKind(ctx context.Context, store *db.DB, service *secrets.Service, kind, from string, apply bool, reportFailure func(string, string, error)) (migrationCounts, error) {
	total := migrationCounts{}
	after := ""
	for {
		rows, err := store.ListSecretMigrationPage(ctx, kind, after)
		if err != nil {
			return total, fmt.Errorf("%s: scan failed: %w", kind, err)
		}
		if len(rows) == 0 {
			return total, nil
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			next, count, err := migrateDocument(ctx, service, kind, row, from, apply)
			fatal := errors.Is(err, secrets.ErrMigrationTarget)
			if err == nil && apply && count > 0 {
				err = store.SaveMigratedSecret(ctx, kind, row, next)
				fatal = err != nil && !errors.Is(err, db.ErrVersionConflict)
			}
			after = row.UUID
			if err != nil {
				total.failed++
				reportFailure(kind, row.UUID, err)
				if fatal || ctx.Err() != nil {
					return total, err
				}
			} else if count > 0 {
				total.success++
			} else {
				total.skipped++
			}
		}
	}
}

func migrateDocument(ctx context.Context, service *secrets.Service, kind string, row db.SecretMigrationRecord, from string, apply bool) (json.RawMessage, int, error) {
	raw := row.Document
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw, 0, nil
	}
	switch kind {
	case "session_resources":
		return migrateGitToken(ctx, service, raw, from, apply)
	case "deployments":
		var emptyArray []json.RawMessage
		if json.Unmarshal(raw, &emptyArray) == nil && len(emptyArray) == 0 {
			return raw, 0, nil
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, 0, errors.New("invalid deployment secrets")
		}
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		total := 0
		for _, key := range keys {
			next, count, err := migrateGitToken(ctx, service, fields[key], from, apply)
			if err != nil {
				return nil, 0, fmt.Errorf("resource index %s: %w", key, err)
			}
			fields[key] = next
			total += count
		}
		next, err := json.Marshal(fields)
		return next, total, err
	default:
		return migrateEnvelope(ctx, service, raw, from, apply)
	}
}

func migrateGitToken(ctx context.Context, service *secrets.Service, raw json.RawMessage, from string, apply bool) (json.RawMessage, int, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw, 0, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, 0, errors.New("invalid Git token envelope")
	}
	if len(fields) == 0 {
		return raw, 0, nil
	}
	if _, legacy := fields["authorization_token"]; legacy {
		return nil, 0, errors.New("legacy plaintext Git token has no encrypted envelope; resubmit it through OMA before migrating")
	}
	if len(fields["envelope"]) == 0 {
		return nil, 0, errors.New("invalid Git token envelope: missing envelope")
	}
	next, count, err := migrateEnvelope(ctx, service, fields["envelope"], from, apply)
	if err != nil {
		return nil, 0, err
	}
	fields["envelope"] = next
	result, err := json.Marshal(fields)
	return result, count, err
}

func migrateEnvelope(ctx context.Context, service *secrets.Service, raw json.RawMessage, from string, apply bool) (json.RawMessage, int, error) {
	envelope, err := decodeEnvelope(raw, from)
	if err != nil {
		return nil, 0, err
	}
	if envelope.KeyProvider == "" {
		return nil, 0, errors.New("invalid secret envelope: missing key_provider")
	}
	if envelope.KeyProvider != from {
		return raw, 0, nil
	}
	if !apply {
		if err := service.CheckMigration(ctx, envelope); err != nil {
			return nil, 0, err
		}
		return raw, 1, nil
	}
	next, err := service.MigrateEnvelope(ctx, envelope)
	if err != nil {
		return nil, 0, err
	}
	result, err := json.Marshal(next)
	return result, 1, err
}

func decodeEnvelope(raw json.RawMessage, from string) (secrets.Envelope, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return secrets.Envelope{}, errors.New("invalid secret envelope")
	}
	for _, names := range [][2]string{
		{"Ciphertext", "ciphertext"}, {"Nonce", "nonce"}, {"WrappedDEK", "wrapped_dek"},
		{"FormatVersion", "format_version"}, {"KeyProvider", "key_provider"}, {"KeyVersion", "key_version"},
	} {
		if value, exists := fields[names[0]]; exists {
			if _, conflict := fields[names[1]]; conflict {
				return secrets.Envelope{}, errors.New("ambiguous secret envelope fields")
			}
			fields[names[1]] = value
			delete(fields, names[0])
		}
	}
	var provider string
	if err := json.Unmarshal(fields["key_provider"], &provider); len(fields["key_provider"]) > 0 && err != nil {
		return secrets.Envelope{}, errors.New("invalid key_provider")
	}
	if provider != "" && provider != from {
		return secrets.Envelope{KeyProvider: provider}, nil
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return secrets.Envelope{}, errors.New("invalid secret envelope")
	}
	var envelope secrets.Envelope
	if err := json.Unmarshal(normalized, &envelope); err != nil {
		return secrets.Envelope{}, errors.New("invalid secret envelope")
	}
	return envelope, nil
}
