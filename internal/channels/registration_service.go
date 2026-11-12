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

const registrationVault = "channel-registration"

const (
	registrationHeartbeatGrace = 45 * time.Second
	registrationKindAuth       = "authorization"
	registrationKindQR         = "qr"
	registrationPhaseWaiting   = "waiting"
	registrationPhaseBound     = "bound"
	registrationNameLimit      = 40
)

func (s *localService) beginAuthorization(ctx context.Context, principal auth.Principal, name string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > registrationNameLimit {
		return nil, invalidChannelName()
	}
	record, url, err := s.launchRegistration(ctx, principal, registrationKindAuth, "", name)
	if err != nil {
		return nil, err
	}
	record.URL = url
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
	record, err = s.settleRegistration(ctx, record)
	if err != nil {
		return nil, err
	}
	return authorizationView(record), nil
}

func (s *localService) qrStatus(ctx context.Context, principal auth.Principal, channelID string) (map[string]any, error) {
	channel, err := s.store.GetChannelRecordByID(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, channelID)
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
	if errors.Is(err, db.ErrNotFound) || record.Phase == "expired" || record.Phase == "denied" || record.Phase == "unknown" {
		if err == nil {
			s.cancelRun(record.ExternalID)
			if err := s.store.DeleteChannelRegistration(ctx, record.ExternalID); err != nil && !errors.Is(err, db.ErrNotFound) {
				return nil, unavailable(err)
			}
		}
		started, url, launchErr := s.launchRegistration(ctx, principal, registrationKindQR, channel.ExternalID, channel.Name)
		if launchErr != nil {
			return nil, launchErr
		}
		started.URL = url
		return qrView(started), nil
	}
	if record.Phase == registrationPhaseWaiting {
		if run := s.activeRun(record.ExternalID); run == nil {
			record, err = s.settleRegistration(ctx, record)
			if err != nil {
				return nil, err
			}
		}
	}
	return qrView(record), nil
}

func (s *localService) launchRegistration(ctx context.Context, principal auth.Principal, kind, channelID, name string) (db.ChannelRegistrationRecord, string, error) {
	prefix := "chra_"
	if kind == registrationKindQR {
		prefix = "chrq_"
	}
	externalID, err := ids.New(prefix)
	if err != nil {
		return db.ChannelRegistrationRecord{}, "", unavailable(err)
	}
	now := time.Now()
	record, err := s.store.CreateChannelRegistration(ctx, db.ChannelRegistrationInput{
		Owner:             s.instanceID,
		ExternalID:        externalID,
		Kind:              kind,
		OrganizationUUID:  principal.OrganizationUUID,
		WorkspaceUUID:     principal.WorkspaceUUID,
		ChannelExternalID: channelID,
		Name:              name,
		Phase:             registrationPhaseWaiting,
		ExpiresAt:         now.Add(registrationDefaultTTL),
		NextPollAt:        now,
		IntervalSeconds:   5,
		Domain:            "feishu",
	})
	if err != nil {
		return db.ChannelRegistrationRecord{}, "", unavailable(err)
	}
	run := s.startRun(record)
	select {
	case start := <-run.start:
		if start.Err != nil {
			return db.ChannelRegistrationRecord{}, "", registrationUnavailable(start.Err)
		}
		return record, start.URL, nil
	case <-time.After(registrationStartTimeout):
		return db.ChannelRegistrationRecord{}, "", registrationUnavailable(errors.New("飞书授权链接生成超时"))
	}
}

func (s *localService) settleRegistration(ctx context.Context, record db.ChannelRegistrationRecord) (db.ChannelRegistrationRecord, error) {
	if record.Phase != registrationPhaseWaiting {
		return record, nil
	}
	if s.activeRun(record.ExternalID) != nil {
		return record, nil
	}
	now := time.Now()
	if !now.Before(record.ExpiresAt) {
		return s.saveRegistrationPhase(ctx, record, "expired")
	}
	if now.Sub(record.HeartbeatAt) < registrationHeartbeatGrace {
		return record, nil
	}
	return s.saveRegistrationPhase(ctx, record, "unknown")
}

func (s *localService) completeRegistration(ctx context.Context, record db.ChannelRegistrationRecord, appID, appSecret string) {
	passed, err := s.credentialsMatch(ctx, appID, appSecret)
	if err != nil {
		s.saveRegistrationPhase(ctx, record, "unknown")
		return
	}
	if !passed {
		s.saveRegistrationPhase(ctx, record, "denied")
		return
	}
	if record.Kind == registrationKindQR {
		if err := s.bindQrChannel(ctx, record, appID, appSecret); err != nil {
			s.logger.WarnContext(ctx, "bind channel qr credentials", "registration_id", record.ExternalID, "error", err)
			s.saveRegistrationPhase(ctx, record, "unknown")
			return
		}
		if err := s.store.DeleteChannelRegistration(ctx, record.ExternalID); err != nil {
			s.logger.WarnContext(ctx, "delete channel qr registration", "registration_id", record.ExternalID, "error", err)
		}
		return
	}
	if s.cipher == nil {
		s.saveRegistrationPhase(ctx, record, "unknown")
		return
	}
	envelope, err := sealRuntimeSecret(ctx, s.cipher, record.OrganizationUUID, record.WorkspaceUUID, registrationVault, record.ExternalID, appSecret)
	if err != nil {
		s.saveRegistrationPhase(ctx, record, "unknown")
		return
	}
	record.Phase = registrationPhaseBound
	record.AppID = appID
	record.AppSecretEnvelope = envelope
	record.DeviceCodeEnvelope = nil
	if _, err := s.store.TransitionChannelRegistration(ctx, registrationUpdate(record, nil)); err != nil {
		s.logger.WarnContext(ctx, "complete channel authorization", "registration_id", record.ExternalID, "error", err)
	}
}

func (s *localService) bindQrChannel(ctx context.Context, record db.ChannelRegistrationRecord, appID, appSecret string) error {
	channel, err := s.store.GetChannelRecordByID(ctx, record.OrganizationUUID, record.WorkspaceUUID, record.ChannelExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return channelNotFound()
	}
	if err != nil {
		return unavailable(err)
	}
	return s.applyChannelCredentials(ctx, channel, appID, appSecret)
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
		Mounts:            channel.Mounts,
		GitTokenEnvelope:  channel.GitTokenEnvelope,
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
	if s.cipher == nil {
		return unavailable(errors.New("secrets service is not configured"))
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
	record, err = s.settleRegistration(ctx, record)
	if err != nil {
		return "", "", err
	}
	if record.Phase != registrationPhaseBound {
		return "", "", authorizationPending()
	}
	if s.cipher == nil || record.AppSecretEnvelope == nil {
		return "", "", unavailable(errors.New("secrets service is not configured"))
	}
	secret, err := openRuntimeSecret(ctx, s.cipher, record.OrganizationUUID, record.WorkspaceUUID, registrationVault, record.ExternalID, *record.AppSecretEnvelope)
	if err != nil {
		return "", "", unavailable(err)
	}
	return record.AppID, secret, nil
}

func (s *localService) saveRegistrationPhase(ctx context.Context, record db.ChannelRegistrationRecord, phase string) (db.ChannelRegistrationRecord, error) {
	record.Phase = phase
	updated, err := s.store.TransitionChannelRegistration(ctx, registrationUpdate(record, record.DeviceCodeEnvelope))
	if errors.Is(err, db.ErrNotFound) {
		current, loadErr := s.store.GetChannelRegistration(ctx, record.OrganizationUUID, record.WorkspaceUUID, record.ExternalID)
		if loadErr != nil {
			return record, unavailable(loadErr)
		}
		return current, nil
	}
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
		Owner:              record.Owner,
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
