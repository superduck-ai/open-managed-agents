package channels

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const (
	registrationVault        = "channel-registration"
	registrationKindAuth     = "authorization"
	registrationKindQR       = "qr"
	registrationPhaseWaiting = "waiting"
	registrationPhaseBound   = "bound"
)

func (s *localService) beginAuthorization(ctx context.Context, principal auth.Principal, name string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > registrationNameLimit {
		return nil, invalidChannelName()
	}
	challenge, err := s.registration.begin(ctx, name)
	if err != nil {
		return nil, registrationUnavailable(err)
	}
	externalID, err := ids.New("chra_")
	if err != nil {
		return nil, unavailable(err)
	}
	deviceEnvelope, err := s.sealRegistrationSecret(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, externalID, challenge.DeviceCode)
	if err != nil {
		return nil, unavailable(err)
	}
	now := time.Now()
	record, err := s.store.CreateChannelRegistration(ctx, db.ChannelRegistrationInput{
		ExternalID:         externalID,
		Kind:               registrationKindAuth,
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		Name:               name,
		Phase:              registrationPhaseWaiting,
		URL:                challenge.URL,
		ExpiresAt:          now.Add(challenge.ExpiresIn),
		NextPollAt:         now.Add(challenge.Interval),
		IntervalSeconds:    int(challenge.Interval / time.Second),
		Domain:             "feishu",
		DeviceCodeEnvelope: deviceEnvelope,
	})
	if err != nil {
		return nil, unavailable(err)
	}
	return authorizationView(record), nil
}

func (s *localService) authorizationStatus(ctx context.Context, principal auth.Principal, id string) (map[string]any, error) {
	record, err := s.store.GetChannelRegistration(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, authorizationNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if record.Kind != registrationKindAuth {
		return nil, authorizationNotFound()
	}
	refreshed, err := s.refreshRegistration(ctx, record)
	if err != nil {
		return nil, err
	}
	return authorizationView(refreshed), nil
}

func (s *localService) qrStatus(ctx context.Context, principal auth.Principal, agentID, channelID string) (map[string]any, error) {
	channel, err := s.store.GetChannelRecord(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID, channelID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, channelNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if channel.BindingStatus == registrationPhaseBound {
		return qrBoundView(), nil
	}
	record, err := s.store.GetChannelQrRegistration(ctx, channelID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, unavailable(err)
	}
	if errors.Is(err, db.ErrNotFound) || record.Phase == "expired" || record.Phase == "denied" {
		return s.startQr(ctx, principal, channel)
	}
	refreshed, err := s.refreshRegistration(ctx, record)
	if err != nil {
		return nil, err
	}
	return qrView(refreshed), nil
}

func (s *localService) startQr(ctx context.Context, principal auth.Principal, channel db.ChannelRecord) (map[string]any, error) {
	challenge, err := s.registration.begin(ctx, channel.Name)
	if err != nil {
		return nil, registrationUnavailable(err)
	}
	externalID, err := ids.New("chrq_")
	if err != nil {
		return nil, unavailable(err)
	}
	deviceEnvelope, err := s.sealRegistrationSecret(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, externalID, challenge.DeviceCode)
	if err != nil {
		return nil, unavailable(err)
	}
	if existing, existingErr := s.store.GetChannelQrRegistration(ctx, channel.ExternalID); existingErr == nil {
		if err := s.store.DeleteChannelRegistration(ctx, existing.ExternalID); err != nil {
			return nil, unavailable(err)
		}
	}
	now := time.Now()
	record, err := s.store.CreateChannelRegistration(ctx, db.ChannelRegistrationInput{
		ExternalID:         externalID,
		Kind:               registrationKindQR,
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		ChannelExternalID:  channel.ExternalID,
		Name:               channel.Name,
		Phase:              registrationPhaseWaiting,
		URL:                challenge.URL,
		ExpiresAt:          now.Add(challenge.ExpiresIn),
		NextPollAt:         now.Add(challenge.Interval),
		IntervalSeconds:    int(challenge.Interval / time.Second),
		Domain:             "feishu",
		DeviceCodeEnvelope: deviceEnvelope,
	})
	if err != nil {
		return nil, unavailable(err)
	}
	return qrView(record), nil
}

func (s *localService) refreshRegistration(ctx context.Context, record db.ChannelRegistrationRecord) (db.ChannelRegistrationRecord, error) {
	switch record.Phase {
	case registrationPhaseBound, "expired", "denied", "unknown":
		return record, nil
	}
	now := time.Now()
	if !now.Before(record.ExpiresAt) {
		return s.saveRegistrationPhase(ctx, record, "expired")
	}
	if now.Before(record.NextPollAt) {
		return record, nil
	}
	deviceCode, err := s.openRegistrationSecret(ctx, record, record.DeviceCodeEnvelope)
	if err != nil {
		return record, unavailable(err)
	}
	poll, err := s.registration.poll(ctx, record.Domain, deviceCode)
	if err != nil {
		return s.saveRegistrationPhase(ctx, record, "unknown")
	}
	switch poll.Status {
	case "authorized":
		return s.finishRegistration(ctx, record, poll)
	case "expired", "denied":
		return s.saveRegistrationPhase(ctx, record, poll.Status)
	case "domain_switched":
		record.Domain = poll.Domain
	case "slow_down":
		record.IntervalSeconds += 5
	}
	record.Phase = registrationPhaseWaiting
	record.NextPollAt = now.Add(time.Duration(record.IntervalSeconds) * time.Second)
	return s.store.UpdateChannelRegistration(ctx, registrationUpdate(record, record.DeviceCodeEnvelope))
}

func (s *localService) finishRegistration(ctx context.Context, record db.ChannelRegistrationRecord, poll registrationPoll) (db.ChannelRegistrationRecord, error) {
	passed, err := s.credentialsMatch(ctx, poll.AppID, poll.AppSecret)
	if err != nil {
		return s.saveRegistrationPhase(ctx, record, "unknown")
	}
	if !passed {
		return s.saveRegistrationPhase(ctx, record, "denied")
	}
	if record.Kind == registrationKindQR {
		if err := s.bindQrChannel(ctx, record, poll.AppID, poll.AppSecret); err != nil {
			return record, err
		}
		if err := s.store.DeleteChannelRegistration(ctx, record.ExternalID); err != nil {
			return record, unavailable(err)
		}
		record.Phase = registrationPhaseBound
		record.AppID = poll.AppID
		record.URL = ""
		return record, nil
	}
	envelope, err := s.sealRegistrationSecret(ctx, record.OrganizationUUID, record.WorkspaceUUID, record.ExternalID, poll.AppSecret)
	if err != nil {
		return record, unavailable(err)
	}
	record.Phase = registrationPhaseBound
	record.AppID = poll.AppID
	record.AppSecretEnvelope = envelope
	record.DeviceCodeEnvelope = nil
	return s.store.UpdateChannelRegistration(ctx, registrationUpdate(record, nil))
}

func (s *localService) bindQrChannel(ctx context.Context, record db.ChannelRegistrationRecord, appID, appSecret string) error {
	channel, err := s.store.GetChannelRecord(ctx, record.OrganizationUUID, record.WorkspaceUUID, "", record.ChannelExternalID)
	if errors.Is(err, db.ErrNotFound) {
		channel, err = s.channelByID(ctx, record)
	}
	if errors.Is(err, db.ErrNotFound) {
		return channelNotFound()
	}
	if err != nil {
		return unavailable(err)
	}
	return s.applyChannelCredentials(ctx, channel, appID, appSecret)
}

func (s *localService) channelByID(ctx context.Context, record db.ChannelRegistrationRecord) (db.ChannelRecord, error) {
	return s.store.GetChannelRecordByID(ctx, record.OrganizationUUID, record.WorkspaceUUID, record.ChannelExternalID)
}

func (s *localService) applyChannelCredentials(ctx context.Context, channel db.ChannelRecord, appID, appSecret string) error {
	if s.cipher == nil {
		return unavailable(errors.New("secrets service is not configured"))
	}
	envelope, err := sealChannelSecret(ctx, s.cipher, channel, appSecret)
	if err != nil {
		return unavailable(err)
	}
	channel.AppID = appID
	channel.AppSecretEnvelope = envelope
	channel.BindingStatus = registrationPhaseBound
	updated, err := s.store.UpdateChannelRecord(ctx, db.ChannelRecordUpdate{
		ExternalID:        channel.ExternalID,
		WorkspaceUUID:     channel.WorkspaceUUID,
		Name:              channel.Name,
		Enabled:           channel.Enabled,
		BindingStatus:     channel.BindingStatus,
		AppID:             channel.AppID,
		AppSecretEnvelope: channel.AppSecretEnvelope,
		IncludeThinking:   channel.IncludeThinking,
		IncludeToolCalls:  channel.IncludeToolCalls,
		ReplyInThread:     channel.ReplyInThread,
	})
	if err != nil {
		return unavailable(err)
	}
	s.reconcile(ctx, updated)
	return nil
}

func (s *localService) applyAuthorization(ctx context.Context, principal auth.Principal, record *db.ChannelRecord, authorizationID string) error {
	appID, secret, err := s.authorizationCredentials(ctx, principal, authorizationID)
	if err != nil {
		return err
	}
	if record.AppID != "" && record.AppID != appID {
		return invalidAppCredentials()
	}
	envelope, err := sealChannelSecret(ctx, s.cipher, *record, secret)
	if err != nil {
		return unavailable(err)
	}
	record.AppID = appID
	record.AppSecretEnvelope = envelope
	record.BindingStatus = registrationPhaseBound
	return nil
}

func (s *localService) authorizationCredentials(ctx context.Context, principal auth.Principal, id string) (string, string, error) {
	record, err := s.store.GetChannelRegistration(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) {
		return "", "", authorizationNotFound()
	}
	if err != nil {
		return "", "", unavailable(err)
	}
	if record.Kind != registrationKindAuth {
		return "", "", authorizationNotFound()
	}
	refreshed, err := s.refreshRegistration(ctx, record)
	if err != nil {
		return "", "", err
	}
	if refreshed.Phase != registrationPhaseBound {
		return "", "", authorizationPending()
	}
	secret, err := s.openRegistrationSecret(ctx, refreshed, refreshed.AppSecretEnvelope)
	if err != nil {
		return "", "", unavailable(err)
	}
	return refreshed.AppID, secret, nil
}

func (s *localService) sealRegistrationSecret(ctx context.Context, organizationUUID, workspaceUUID, key, plaintext string) (*string, error) {
	if s.cipher == nil {
		return nil, errors.New("secrets service is not configured")
	}
	return sealRuntimeSecret(ctx, s.cipher, organizationUUID, workspaceUUID, registrationVault, key, plaintext)
}

func (s *localService) openRegistrationSecret(ctx context.Context, record db.ChannelRegistrationRecord, encoded *string) (string, error) {
	if s.cipher == nil {
		return "", errors.New("secrets service is not configured")
	}
	if encoded == nil || *encoded == "" {
		return "", errors.New("授权记录缺少设备码")
	}
	return openRuntimeSecret(ctx, s.cipher, record.OrganizationUUID, record.WorkspaceUUID, registrationVault, record.ExternalID, *encoded)
}

func (s *localService) saveRegistrationPhase(ctx context.Context, record db.ChannelRegistrationRecord, phase string) (db.ChannelRegistrationRecord, error) {
	record.Phase = phase
	updated, err := s.store.UpdateChannelRegistration(ctx, registrationUpdate(record, record.DeviceCodeEnvelope))
	if err != nil {
		return record, unavailable(err)
	}
	return updated, nil
}

func registrationUpdate(record db.ChannelRegistrationRecord, deviceCode *string) db.ChannelRegistrationUpdate {
	return db.ChannelRegistrationUpdate{
		ExternalID:         record.ExternalID,
		Phase:              record.Phase,
		URL:                record.URL,
		ExpiresAt:          record.ExpiresAt,
		NextPollAt:         record.NextPollAt,
		IntervalSeconds:    record.IntervalSeconds,
		Domain:             record.Domain,
		DeviceCodeEnvelope: deviceCode,
		AppID:              record.AppID,
		AppSecretEnvelope:  record.AppSecretEnvelope,
	}
}

func authorizationView(record db.ChannelRegistrationRecord) map[string]any {
	view := map[string]any{
		"id":         record.ExternalID,
		"status":     registrationStatus(record.Phase),
		"url":        record.URL,
		"expires_at": record.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if record.AppID != "" {
		view["app_id"] = record.AppID
	}
	return view
}

func qrView(record db.ChannelRegistrationRecord) map[string]any {
	return map[string]any{
		"status":     registrationStatus(record.Phase),
		"url":        record.URL,
		"expires_at": record.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func qrBoundView() map[string]any {
	return map[string]any{"status": "bound", "url": "", "expires_at": ""}
}

func registrationStatus(phase string) string {
	switch phase {
	case registrationPhaseWaiting:
		return "pending"
	case "denied":
		return "error"
	case registrationPhaseBound, "expired", "unknown":
		return phase
	default:
		return "unknown"
	}
}
