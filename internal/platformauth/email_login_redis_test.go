package platformauth

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestRedisEmailCodeStoreIntegration(t *testing.T) {
	redisURL, ok := os.LookupEnv("REDIS_URL")
	if !ok {
		t.Skip("REDIS_URL is not set")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse REDIS_URL: %v", err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(t.Context()).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}

	suffix := uuid.NewString()
	emailHash := "integration-email-" + suffix
	revokeEmailHash := "integration-revoke-email-" + suffix
	keys := []string{
		authKey("email-code", emailHash),
		authKey("email-code-cooldown", emailHash),
		authKey("email-code-rate", emailHash),
		authKey("email-code", revokeEmailHash),
		authKey("email-code-cooldown", revokeEmailHash),
		authKey("email-code-rate", revokeEmailHash),
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Del(ctx, keys...).Err()
	})

	store := newRedisEmailCodeStore(client)
	issue := EmailCodeIssue{
		EmailHash:   emailHash,
		ChallengeID: suffix,
		Digest:      "expected-digest",
	}
	if err := store.Issue(t.Context(), issue); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if err := store.Issue(t.Context(), issue); !errors.Is(err, errEmailLoginRateLimited) {
		t.Fatalf("Issue(during cooldown) error = %v, want rate limited", err)
	}
	if err := store.Check(t.Context(), emailHash, "wrong-digest"); !errors.Is(err, errEmailLoginCodeInvalid) {
		t.Fatalf("Check(wrong) error = %v, want invalid", err)
	}
	if err := store.Check(t.Context(), emailHash, issue.Digest); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if err := store.Consume(t.Context(), emailHash, issue.Digest); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if err := store.Check(t.Context(), emailHash, issue.Digest); !errors.Is(err, errEmailLoginCodeInvalid) {
		t.Fatalf("Check(consumed) error = %v, want invalid", err)
	}

	issue.EmailHash = revokeEmailHash
	for attempt := range emailSendLimit + 1 {
		issue.ChallengeID = strconv.Itoa(attempt) + "-" + suffix
		if err := store.Issue(t.Context(), issue); err != nil {
			t.Fatalf("Issue(revoke attempt %d) error = %v, want failed sends not rate limited", attempt, err)
		}
		if err := store.Revoke(t.Context(), revokeEmailHash, issue.ChallengeID); err != nil {
			t.Fatalf("Revoke(attempt %d) error = %v", attempt, err)
		}
	}

	for i := range 21 {
		distinctEmailHash := "integration-distinct-email-" + strconv.Itoa(i) + "-" + suffix
		keys = append(keys,
			authKey("email-code", distinctEmailHash),
			authKey("email-code-cooldown", distinctEmailHash),
			authKey("email-code-rate", distinctEmailHash),
		)
		distinctIssue := EmailCodeIssue{
			EmailHash:   distinctEmailHash,
			ChallengeID: strconv.Itoa(i) + "-" + suffix,
			Digest:      "distinct-digest",
		}
		if err := store.Issue(t.Context(), distinctIssue); err != nil {
			t.Fatalf("Issue(distinct email %d) error = %v, want independent email limits", i, err)
		}
	}
}
