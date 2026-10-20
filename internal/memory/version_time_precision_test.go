package memory

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestVersionCreatedAtRoundTripsInclusiveBounds(t *testing.T) {
	for _, timestamp := range []string{"2026-10-18T12:32:56.123456Z", "2026-10-18T20:32:56.987654+08:00", "2026-10-18T12:32:56Z"} {
		t.Run(timestamp, func(t *testing.T) {
			created, err := time.Parse(time.RFC3339Nano, timestamp)
			if err != nil {
				t.Fatal(err)
			}
			for _, content := range []*string{nil, new(string)} {
				response := responseFromVersionRecord(db.MemoryVersion{CreatedAt: created}, content)
				query := url.Values{"created_at[gte]": {response.CreatedAt}, "created_at[lte]": {response.CreatedAt}}
				request := httptest.NewRequest("GET", "/?"+query.Encode(), nil)
				lower, err := parseOptionalTime(request, "created_at[gte]")
				if err != nil || lower == nil {
					t.Fatalf("invalid lower boundary: %v", err)
				}
				upper, err := parseOptionalTime(request, "created_at[lte]")
				if err != nil || upper == nil {
					t.Fatalf("invalid upper boundary: %v", err)
				}
				if !lower.Equal(created) || !upper.Equal(created) || created.Before(*lower) || created.After(*upper) {
					t.Fatalf("public created_at=%s excludes actual version time=%s", response.CreatedAt, timestamp)
				}
			}
		})
	}
}
