//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/devinit"
)

func TestChatBrainImages(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	providers, err := devinit.LoadModelProviders(os.Getenv("VERIFY_BE_VISION_PROVIDERS"))
	requireOK(t, err)
	model := os.Getenv("VERIFY_BE_VISION_MODEL")
	e := newLiveEnv(t)
	for _, provider := range providers.Providers {
		if !slices.Contains(provider.ModelIDs, model) {
			continue
		}
		catalog := isolatedWorkerProviders(t, e)
		if len(catalog) != 1 || catalog[0].Name != isolatedProviderName {
			t.Fatal("vision requires the isolated fixture provider")
		}
		e.request(t, "PUT", isolatedProviderPath(e)+"/"+catalog[0].ID, "", map[string]any{
			"name": isolatedProviderName, "slug": "default", "api_backend": provider.APIBackend, "base_url": provider.BaseURL, "api_key": provider.APIKey,
			"models": []any{map[string]any{"id": model, "context_window": 128000, "max_output_tokens": 4096, "input_modalities": []string{"text", "image"}}},
		}, 200, e.cookies...)
		t.Logf("vision model=%s backend=%s", model, provider.APIBackend)
	}
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion, "model": map[string]string{"id": model},
		"system": "Follow the user's image inspection request. Use Read when a file path is provided. Reply with only the requested uppercase color word.",
		"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": false},
			"configs": []any{map[string]any{"name": "read", "enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}}},
	}, 200)
	first, err := rand.Int(rand.Reader, big.NewInt(4))
	requireOK(t, err)
	second := (first.Int64() + 1) % 4
	messageImage, messageColor := visionColorPNG(t, int(first.Int64()))
	readImage, readColor := visionColorPNG(t, int(second))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="vision.png"`}, "Content-Type": {"image/png"}})
	requireOK(t, err)
	_, err = part.Write(readImage)
	requireOK(t, err)
	requireOK(t, writer.Close())
	var file struct {
		ID string `json:"id"`
	}
	upload, err := http.NewRequestWithContext(t.Context(), http.MethodPost, e.url+"/v1/files?beta=true", &body)
	requireOK(t, err)
	upload.Header.Set("Authorization", "Bearer "+e.apiKey)
	upload.Header.Set("Anthropic-Beta", "files-api-2025-04-14")
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	uploaded, err := http.DefaultClient.Do(upload)
	requireOK(t, err)
	defer uploaded.Body.Close()
	if uploaded.StatusCode != http.StatusOK {
		t.Fatalf("vision file upload status=%d", uploaded.StatusCode)
	}
	requireOK(t, json.NewDecoder(uploaded.Body).Decode(&file))
	t.Cleanup(func() { e.requestContext(context.Background(), t, "DELETE", "/v1/files/"+file.ID, e.apiKey, nil, 200) })
	cfg, err := config.Load()
	requireOK(t, err)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	node := startBrainNode(t, e, cfg, "vision")
	defer node.stop(t)
	var created struct {
		ID string `json:"id"`
	}
	ebody := map[string]any{"agent": e.agent.ExternalID, "environment_id": e.environment.ExternalID,
		"resources": []any{map[string]string{"type": "file", "file_id": file.ID, "mount_path": "/uploads/vision.png"}}}
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/sessions", e.apiKey, ebody, 200), &created))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "POST", "/v1/sessions/"+created.ID+"/archive", e.apiKey, nil, 200)
		e.requestContext(context.Background(), t, "DELETE", "/v1/sessions/"+created.ID, e.apiKey, nil, 200)
	})
	session, found, err := e.database.GetSession(t.Context(), e.key.WorkspaceUUID.String(), created.ID)
	requireOK(t, err)
	if !found {
		t.Fatal("vision Session not found")
	}
	f := &liveSession{env: e, session: session}
	e.request(t, "POST", "/v1/sessions/"+created.ID+"/events", e.apiKey, map[string]any{"events": []any{map[string]any{"type": "user.message", "content": []any{
		map[string]string{"type": "text", "text": "Identify the color of the large square in this attached image. Reply with exactly one word: RED, GREEN, BLUE, or YELLOW. Do not use tools."},
		map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(messageImage)}},
	}}}}, 200)
	waitVisionAnswer(t, f, 1, messageColor)
	f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), created.ID)
	requireOK(t, err)
	waitChatIdle(t, f, 1)
	chatProof(t, started, "message_image_seen")
	submitChat(t, f, "Use Read to open /mnt/session/uploads/vision.png. Identify the color of the large square in THAT file, which is a different image from the previous attachment. Reply with exactly one word: RED, GREEN, BLUE, or YELLOW.")
	waitVisionAnswer(t, f, 2, readColor)
	waitChatIdle(t, f, 2)
	if provider.created.Load() != 1 {
		t.Fatalf("Read sandbox count=%d, want 1", provider.created.Load())
	}
	_ = waitMemorySandbox(t, e, f)
	registerPublicSandboxCleanup(t, e, f)
	if !slices.ContainsFunc(chatHistory(t, f), func(event chatEvent) bool { return event.Type == "agent.tool_use" && event.Name == "read" }) {
		t.Fatal("vision answer did not execute Read")
	}
	chatProof(t, started, "read_image_seen")
	assertVisionJournal(t, f, messageImage, readImage)
	chatProof(t, started, "native_images_persisted")
}

func visionColorPNG(t *testing.T, index int) ([]byte, string) {
	t.Helper()
	palette := []color.RGBA{{R: 255, A: 255}, {G: 180, A: 255}, {B: 255, A: 255}, {R: 255, G: 230, A: 255}}
	names := []string{"RED", "GREEN", "BLUE", "YELLOW"}
	canvas := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(32, 32, 224, 224), image.NewUniform(palette[index]), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	requireOK(t, png.Encode(&encoded, canvas))
	return encoded.Bytes(), names[index]
}

func waitVisionAnswer(t *testing.T, f *liveSession, count int, expected string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var answers []string
		for _, event := range chatHistory(t, f) {
			if event.Type == "session.error" {
				t.Fatal("vision Session reported an error")
			}
			if event.Type == "agent.message" {
				var text strings.Builder
				for _, block := range event.Content {
					if block.Type == "text" {
						text.WriteString(block.Text)
					}
				}
				answers = append(answers, text.String())
			}
		}
		if len(answers) >= count {
			if strings.TrimSpace(answers[count-1]) != expected {
				t.Fatalf("vision turn %d answer=%q, expected=%q", count, answers[count-1], expected)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("vision answer timed out")
		case <-ticker.C:
		}
	}
}

type visionJournalBlock struct {
	Type       string `json:"type"`
	Attachment struct {
		ID    string `json:"attachmentId"`
		Bytes int64  `json:"bytes"`
	} `json:"attachment"`
}

func assertVisionJournal(t *testing.T, f *liveSession, originals ...[]byte) {
	t.Helper()
	events, _, err := f.env.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionExternalID: f.code.ExternalID, Limit: 500})
	requireOK(t, err)
	seen := map[string]bool{}
	for _, event := range events {
		for _, original := range originals {
			if bytes.Contains(event.Payload, []byte(base64.StdEncoding.EncodeToString(original))) {
				t.Fatal("original image bytes entered private history")
			}
		}
		var envelope struct {
			Type string `json:"type"`
			Data struct {
				Type string `json:"type"`
				Data struct {
					Content []visionJournalBlock `json:"content"`
					Message struct {
						Content []visionJournalBlock `json:"content"`
					} `json:"message"`
				} `json:"data"`
			} `json:"data"`
		}
		requireOK(t, json.Unmarshal(event.Payload, &envelope))
		if envelope.Type != "dsh_log" {
			continue
		}
		blocks := append(envelope.Data.Data.Content, envelope.Data.Data.Message.Content...)
		for _, block := range blocks {
			if block.Type != "image" {
				continue
			}
			record, err := f.env.database.GetBrainImageAttachment(t.Context(), f.code.WorkspaceUUID, f.session.UUID, strings.TrimPrefix(block.Attachment.ID, "sha256:"))
			requireOK(t, err)
			object, err := f.env.objects.Open(t.Context(), record.ObjectKey, nil)
			requireOK(t, err)
			data, err := io.ReadAll(object.Body)
			requireOK(t, err)
			requireOK(t, object.Body.Close())
			digest := sha256.Sum256(data)
			if "sha256:"+hex.EncodeToString(digest[:]) != block.Attachment.ID || int64(len(data)) != block.Attachment.Bytes {
				t.Fatal("remote native image integrity mismatch")
			}
			seen[envelope.Data.Type] = true
		}
	}
	if !seen["user/message"] || !seen["tool/result"] {
		t.Fatalf("native image journal missing user or tool references: %v", seen)
	}
}
