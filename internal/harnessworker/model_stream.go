package harnessworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness/fantasyadapter"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type modelFactsTransport struct {
	base http.RoundTripper
	body *modelFactsBody
	open func() error
	key  string
}

type modelFactsBody struct {
	io.ReadCloser
	tail bytes.Buffer
}

func (b *modelFactsBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	const tailLimit = 16 << 10
	b.tail.Write(data[max(0, n-tailLimit):n])
	if discard := b.tail.Len() - tailLimit; discard > 0 {
		b.tail.Next(discard)
	}
	return n, err
}

func (t *modelFactsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set(sessioncontract.HarnessModelStreamHeader, t.key)
	response, err := t.base.RoundTrip(request)
	if err == nil {
		t.body = &modelFactsBody{ReadCloser: response.Body}
		response.Body = t.body
		if err := t.open(); err != nil {
			_ = response.Body.Close()
			return nil, err
		}
	}
	return response, err
}

func (t *modelFactsTransport) facts() (codesessions.HarnessModelFacts, error) {
	if t.body == nil {
		return codesessions.HarnessModelFacts{}, errors.New("model gateway did not respond")
	}
	data := t.body.tail.Bytes()
	marker := []byte("\n" + sessioncontract.HarnessModelFactsComment + t.key + " ")
	start := bytes.LastIndex(data, marker)
	if start < 0 {
		return codesessions.HarnessModelFacts{}, errors.New("model gateway did not return completion facts")
	}
	encoded, suffix, found := bytes.Cut(data[start+len(marker):], []byte("\n\n"))
	if !found || len(suffix) != 0 {
		return codesessions.HarnessModelFacts{}, errors.New("model gateway returned incomplete completion facts")
	}
	raw, err := base64.RawURLEncoding.DecodeString(string(encoded))
	if err != nil {
		return codesessions.HarnessModelFacts{}, err
	}
	var facts codesessions.HarnessModelFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		return codesessions.HarnessModelFacts{}, err
	}
	if facts.EndedAt.IsZero() {
		return codesessions.HarnessModelFacts{}, errors.New("model gateway did not return an end time")
	}
	return facts, nil
}

type workerModelBlock struct {
	index    int
	thinking bool
	text     strings.Builder
}

type workerModelStream struct {
	encoder *json.Encoder
	blocks  map[int]*workerModelBlock
}

func (s *workerModelStream) observe(kind, id, delta string) error {
	index, err := strconv.Atoi(id)
	if err != nil || index < 0 || index >= 4096 {
		return errors.New("model stream has an invalid block index")
	}
	block := s.blocks[index]
	if block == nil {
		block = &workerModelBlock{index: index, thinking: kind != codesessions.HarnessTextDelta}
		s.blocks[index] = block
	}
	frame := codesessions.HarnessModelFrame{Type: kind, Index: index}
	if kind == codesessions.HarnessTextDelta {
		block.text.WriteString(delta)
		frame.Text = delta
	}
	return s.encoder.Encode(frame)
}

func (s *workerModelStream) finish(facts codesessions.HarnessModelFacts, call cloudharness.ToolCall, next codesessions.HarnessModelNext) error {
	final := codesessions.HarnessModelFinal{Facts: facts, Call: call, Next: next}
	if facts.ErrorType == "" {
		for _, block := range s.blocks {
			final.Blocks = append(final.Blocks, codesessions.HarnessModelBlock{Index: block.index, Thinking: block.thinking, Text: block.text.String()})
		}
		slices.SortFunc(final.Blocks, func(a, b codesessions.HarnessModelBlock) int { return a.Index - b.Index })
	}
	return s.encoder.Encode(codesessions.HarnessModelFrame{Type: codesessions.HarnessModelFinish, Result: &final})
}

func (s *workerModelStream) commit(facts codesessions.HarnessModelFacts, transition cloudharness.Transition) error {
	switch work := transition.Pending.(type) {
	case nil:
		return s.finish(facts, cloudharness.ToolCall{}, codesessions.HarnessModelDone)
	case cloudharness.ToolWork:
		return s.finish(facts, work.Call, codesessions.HarnessModelTool)
	case cloudharness.UserChoiceWork:
		return s.finish(facts, work.Call, codesessions.HarnessModelUserChoice)
	default:
		return fmt.Errorf("unsupported model transition to %T", transition.Pending)
	}
}

func (w *Worker) runModelStream(ctx context.Context, claim db.HarnessTurnClaim, token string, config modelConfig, tools []fantasy.Tool, history []cloudharness.Message, input cloudharness.StepInput) error {
	client := llmproviders.NewHTTPClient(0)
	digest := sha256.Sum256([]byte(token))
	capture := &modelFactsTransport{base: client.Transport, key: "1:" + hex.EncodeToString(digest[:])}
	client.Transport = capture
	model, err := w.model(ctx, claim.Lease.Ref.UUID, token, config, tools, client)
	if err != nil {
		return err
	}

	reader, writer := io.Pipe()
	defer writer.Close()
	eventsCtx, cancelEvents := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelEvents()
	stop := context.AfterFunc(ctx, func() {
		timer := time.NewTimer(settleTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancelEvents()
		case <-eventsCtx.Done():
		}
	})
	defer stop()

	done := make(chan error, 1)
	go func() {
		err := w.sendModelStream(eventsCtx, claim, token, reader)
		_ = reader.CloseWithError(err)
		done <- err
	}()
	stream := workerModelStream{encoder: json.NewEncoder(writer), blocks: make(map[int]*workerModelBlock)}
	opened := false
	capture.open = func() error {
		if opened {
			return nil
		}
		opened = true
		return stream.encoder.Encode(codesessions.HarnessModelFrame{Type: codesessions.HarnessModelOpen})
	}
	model = model.WithStreamCallbacks(fantasyadapter.StreamCallbacks{
		OnTextDelta: func(id, text string) error {
			return stream.observe(codesessions.HarnessTextDelta, id, text)
		},
		OnReasoningStart: func(id string, _ fantasy.ReasoningContent) error {
			return stream.observe(codesessions.HarnessThinkingStart, id, "")
		},
		OnReasoningEnd: func(id string, _ fantasy.ReasoningContent) error {
			return stream.observe(codesessions.HarnessThinkingEnd, id, "")
		},
	})
	var facts codesessions.HarnessModelFacts
	attempted, finalized := false, false
	task := cloudharness.ClaimedTask{
		ModelPolicy: codesessions.HarnessModelWork(claim.Subagent != nil),
		CompleteModel: func(ctx context.Context) (cloudharness.ModelCompleted, error) {
			attempted = true
			result, modelErr := model.Complete(ctx, history)
			var factsErr error
			facts, factsErr = capture.facts()
			if factsErr != nil {
				facts = codesessions.HarnessModelFacts{EndedAt: time.Now().UTC().Truncate(time.Microsecond), ErrorType: "incomplete_response"}
			}
			if err := errors.Join(modelErr, capture.open(), factsErr); err != nil {
				return cloudharness.ModelCompleted{}, err
			}
			if facts.ErrorType != "" {
				return cloudharness.ModelCompleted{}, fmt.Errorf("model gateway reported %s", facts.ErrorType)
			}
			return result, nil
		},
		CommitModel: func(_ context.Context, transition cloudharness.Transition) error {
			finalized = true
			finishErr := stream.commit(facts, transition)
			_ = writer.Close()
			return errors.Join(finishErr, <-done)
		},
	}
	if result, resumed := input.Message.Content.(cloudharness.ToolResult); resumed {
		task.Work = cloudharness.ToolWork{Call: cloudharness.ToolCall{ID: result.CallID}}
	}
	stepErr := stepClaimedTask(ctx, task, input)
	var finishErr error
	if !finalized {
		if attempted {
			if facts.ErrorType == "" {
				facts.ErrorType = "invalid_response"
			}
			finishErr = stream.finish(facts, cloudharness.ToolCall{}, "")
		}
		_ = writer.Close()
		finishErr = errors.Join(finishErr, <-done)
	}
	if err := errors.Join(stepErr, finishErr); err != nil {
		return fmt.Errorf("harness model stream: %w", err)
	}
	return nil
}

func (w *Worker) sendModelStream(ctx context.Context, claim db.HarnessTurnClaim, token string, body io.Reader) error {
	endpoint, err := url.JoinPath(w.apiURL, "v1", "code", "sessions", claim.CodeSession.ExternalID, "worker", "events")
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/x-ndjson")
	request.Header.Set(sessioncontract.HarnessModelStreamHeader, "1")
	request.Header.Set(sessioncontract.HarnessTurnHeader, claim.Lease.Ref.UUID)
	response, err := llmproviders.NewHTTPClient(0).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("harness worker event ingestion returned HTTP %d", response.StatusCode)
	}
	return nil
}
