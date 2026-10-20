package metering

import (
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Store interface {
	InsertUsageRecord(context.Context, db.UsageRecord) error
}

type Transport struct {
	Base       http.RoundTripper
	Store      Store
	Calculator *billing.Calculator
	Source     string
	Logger     *slog.Logger
}

type modelKey struct{}

func WithModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, modelKey{}, model)
}

func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	principal, ok := auth.PrincipalFromContext(request.Context())
	if !ok || principal.CodeSessionExternalID != "" {
		return t.Base.RoundTrip(request)
	}
	record, err := requestRecord(request, principal, t.Source)
	if err != nil {
		return nil, err
	}
	calculator, err := t.Calculator.Scope(request.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, record.Model)
	if err != nil {
		return nil, err
	}
	if price := calculator.PriceSnapshot(record.Model); price != nil {
		record.PriceSnapshot, _ = json.Marshal(price)
	}
	if err := t.Store.InsertUsageRecord(request.Context(), record); err != nil {
		return nil, err
	}
	request = request.Clone(request.Context())
	request.Header.Del("Accept-Encoding")
	observer := &observation{}
	finish := sync.OnceFunc(func() { t.finish(request.Context(), record, observer) })
	response, err := t.Base.RoundTrip(request)
	if err != nil {
		finish()
		return nil, err
	}
	observer.streaming = strings.Contains(response.Header.Get("Content-Type"), "text/event-stream")
	observer.failed = response.StatusCode < 200 || response.StatusCode >= 300
	response.Body = &observedBody{ReadCloser: response.Body, observer: observer, finish: finish}
	return response, nil
}

func requestRecord(request *http.Request, principal auth.Principal, source string) (db.UsageRecord, error) {
	now := time.Now().UTC()
	record := db.UsageRecord{OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		AttemptID: uuid.NewV4().String(), Source: source, StartedAt: now, EndedAt: now, Version: "request_cents_v1"}
	if principal.APIKeyUUID != "" {
		record.APIKeyUUID = new(principal.APIKeyUUID)
	}
	if principal.UserUUID != "" {
		record.UserUUID = new(principal.UserUUID)
	}
	if request.GetBody == nil {
		return record, missingRequestBody()
	}
	body, err := request.GetBody()
	if err != nil {
		return record, err
	}
	defer body.Close()
	var input struct {
		Model string `json:"model"`
	}
	if err := json.UnmarshalRead(body, &input); err != nil {
		return record, err
	}
	record.Model, _ = request.Context().Value(modelKey{}).(string)
	if record.Model == "" {
		record.Model = input.Model
	}
	return record, nil
}

func (t *Transport) finish(ctx context.Context, record db.UsageRecord, observer *observation) {
	observer.end()
	record.EndedAt, record.Finalized = time.Now().UTC(), true
	db.ApplyUsage(&record, observer.usage)
	record.Complete = observer.complete && !observer.failed && observer.usage.Complete()
	var price billing.ModelPrice
	calculator := billing.NewCalculator(nil)
	if len(record.PriceSnapshot) > 0 && json.Unmarshal(record.PriceSnapshot, &price) == nil {
		calculator = billing.NewCalculator(map[string]billing.ModelPrice{record.Model: price})
	}
	if cents, ok := calculator.ModelRequestCents(record.Model, observer.usage.Tokens()); ok && observer.usage.InputTokens != nil {
		record.ModelCents = new(cents)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for {
		err := t.Store.InsertUsageRecord(cleanup, record)
		if err == nil {
			return
		}
		select {
		case <-cleanup.Done():
			t.Logger.ErrorContext(cleanup, "persist usage record", "attempt_id", record.AttemptID, "error", err)
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type observedBody struct {
	io.ReadCloser
	observer *observation
	finish   func()
}

func (b *observedBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	b.observer.write(data[:n])
	if err != nil {
		if err != io.EOF {
			b.observer.failed = true
		}
		b.finish()
	}
	return n, err
}

func (b *observedBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish()
	return err
}
