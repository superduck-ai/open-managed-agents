package platformauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	emailCodeTTL            = 10 * time.Minute
	emailCodeResendInterval = time.Minute
	emailCodeMaxAttempts    = 5
	emailSendLimit          = 5
	sendRateWindow          = time.Hour
	loginCodeUpperBound     = 1_000_000
)

type LoginCodeSender interface {
	SendLoginCode(ctx context.Context, recipient, code string) error
}

type EmailCodeIssue struct {
	EmailHash   string
	ChallengeID string
	Digest      string
}

type EmailCodeStore interface {
	Issue(ctx context.Context, issue EmailCodeIssue) error
	Verify(ctx context.Context, emailHash, digest string) error
	Revoke(ctx context.Context, emailHash, challengeID string) error
}

type redisEmailCodeStore struct {
	client *redis.Client
}

func newRedisEmailCodeStore(client *redis.Client) *redisEmailCodeStore {
	return &redisEmailCodeStore{client: client}
}

var issueEmailCodeScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[2]) == 1 then
  return 1
end
local email_count = tonumber(redis.call("GET", KEYS[3]) or "0")
if email_count >= tonumber(ARGV[5]) then
  return 1
end
email_count = redis.call("INCR", KEYS[3])
if email_count == 1 then
  redis.call("PEXPIRE", KEYS[3], ARGV[4])
end
redis.call("HSET", KEYS[1], "id", ARGV[1], "digest", ARGV[2], "attempts", 0)
redis.call("PEXPIRE", KEYS[1], ARGV[3])
redis.call("SET", KEYS[2], "1", "PX", ARGV[6])
return 0
`)

var verifyEmailCodeScript = redis.NewScript(`
local digest = redis.call("HGET", KEYS[1], "digest")
if not digest then
  return 1
end
local attempts = tonumber(redis.call("HGET", KEYS[1], "attempts") or "0")
if attempts >= tonumber(ARGV[2]) then
  redis.call("DEL", KEYS[1])
  return 1
end
if digest ~= ARGV[1] then
  attempts = redis.call("HINCRBY", KEYS[1], "attempts", 1)
  if attempts >= tonumber(ARGV[2]) then
    redis.call("DEL", KEYS[1])
  end
  return 1
end
redis.call("DEL", KEYS[1])
return 0
`)

var revokeEmailCodeScript = redis.NewScript(`
if redis.call("HGET", KEYS[1], "id") == ARGV[1] then
  return redis.call("DEL", KEYS[1], KEYS[2])
end
return 0
`)

func (s *redisEmailCodeStore) Issue(ctx context.Context, issue EmailCodeIssue) error {
	keys := []string{
		authKey("email-code", issue.EmailHash),
		authKey("email-code-cooldown", issue.EmailHash),
		authKey("email-code-rate", issue.EmailHash),
	}
	result, err := issueEmailCodeScript.Run(ctx, s.client, keys,
		issue.ChallengeID,
		issue.Digest,
		emailCodeTTL.Milliseconds(),
		sendRateWindow.Milliseconds(),
		emailSendLimit,
		emailCodeResendInterval.Milliseconds(),
	).Int64()
	if err != nil {
		return err
	}
	if result != 0 {
		return errEmailLoginRateLimited
	}
	return nil
}

func (s *redisEmailCodeStore) Verify(ctx context.Context, emailHash, digest string) error {
	result, err := verifyEmailCodeScript.Run(ctx, s.client, []string{authKey("email-code", emailHash)}, digest, emailCodeMaxAttempts).Int64()
	if err != nil {
		return err
	}
	if result != 0 {
		return errEmailLoginCodeInvalid
	}
	return nil
}

func (s *redisEmailCodeStore) Revoke(ctx context.Context, emailHash, challengeID string) error {
	keys := []string{authKey("email-code", emailHash), authKey("email-code-cooldown", emailHash)}
	return revokeEmailCodeScript.Run(ctx, s.client, keys, challengeID).Err()
}

func (s *EmailProvider) RequestEmailLogin(ctx context.Context, rawEmail string) error {
	email, err := normalizeLoginEmail(rawEmail)
	if err != nil {
		return invalidEmail(err)
	}
	if s == nil || s.codes == nil || s.sender == nil || len(s.codeHMACKey) == 0 {
		return emailLoginUnavailable("Could not send a code. Try again", errors.New("email login is not configured"))
	}

	code, err := newLoginCode()
	if err != nil {
		return emailLoginUnavailable("Could not send a code. Try again", err)
	}
	challengeID := randomToken(16)
	emailHash := s.digest("email", email)
	issue := EmailCodeIssue{
		EmailHash:   emailHash,
		ChallengeID: challengeID,
		Digest:      s.digest("code", email, code),
	}
	if err := s.codes.Issue(ctx, issue); err != nil {
		if errors.Is(err, errEmailLoginRateLimited) {
			return emailLoginRateLimited(err)
		}
		s.logger.ErrorContext(ctx, "issue email login code", "error", err)
		return emailLoginUnavailable("Could not send a code. Try again", err)
	}

	if err := s.sender.SendLoginCode(ctx, email, code); err != nil {
		if revokeErr := s.codes.Revoke(context.WithoutCancel(ctx), emailHash, challengeID); revokeErr != nil {
			s.logger.ErrorContext(ctx, "revoke unsent email login code", "error", revokeErr)
		}
		s.logger.ErrorContext(ctx, "send email login code", "error", err)
		return emailLoginUnavailable("Could not send a code. Try again", err)
	}
	return nil
}

func (s *EmailProvider) VerifyEmailLogin(ctx context.Context, rawEmail, code string) (string, string, error) {
	email, err := normalizeLoginEmail(rawEmail)
	if err != nil {
		return "", "", invalidEmail(err)
	}
	if !validLoginCode(code) {
		return "", "", invalidLoginCode(errEmailLoginCodeInvalid)
	}
	if s == nil || s.codes == nil || len(s.codeHMACKey) == 0 {
		return "", "", emailLoginUnavailable("Could not verify that code. Try again", errors.New("email login is not configured"))
	}
	if err := s.codes.Verify(ctx, s.digest("email", email), s.digest("code", email, code)); err != nil {
		if errors.Is(err, errEmailLoginCodeInvalid) {
			return "", "", invalidLoginCode(err)
		}
		s.logger.ErrorContext(ctx, "verify email login code", "error", err)
		return "", "", emailLoginUnavailable("Could not verify that code. Try again", err)
	}
	userID, orgUUID, err := s.findOrCreateUserContextByEmail(ctx, email)
	if err != nil {
		s.logger.ErrorContext(ctx, "provision verified email login", "error", err)
		return "", "", emailLoginUnavailable("Could not complete sign in. Try again", err)
	}
	return userID, orgUUID, nil
}

func normalizeLoginEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", errors.New("invalid email address")
	}
	return email, nil
}

func validLoginCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, value := range code {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}

func newLoginCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(loginCodeUpperBound))
	if err != nil {
		return "", fmt.Errorf("generate login code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func (s *EmailProvider) digest(parts ...string) string {
	digest := hmac.New(sha256.New, s.codeHMACKey)
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func authKey(suffix, hash string) string {
	return "platform:auth:" + suffix + ":" + hash
}
