package channels

import (
	"context"
	"errors"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	registrationStartTimeout = 15 * time.Second
	registrationDefaultTTL   = 10 * time.Minute
)

type appRegistrar interface {
	RegisterApp(ctx context.Context, opts *registration.Options) (*registration.RegisterAppResult, error)
}

type sdkRegistrar struct{}

func (sdkRegistrar) RegisterApp(ctx context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
	return registration.RegisterApp(ctx, opts)
}

type registrationStart struct {
	URL string
	Err error
}

type registrationRun struct {
	cancel context.CancelFunc
	start  chan registrationStart
}

func (r *registrationRun) signal(start registrationStart) {
	select {
	case r.start <- start:
	default:
	}
}

func (s *localService) startRun(record db.ChannelRegistrationRecord) *registrationRun {
	ctx, cancel := context.WithCancel(context.Background())
	run := &registrationRun{cancel: cancel, start: make(chan registrationStart, 1)}
	s.runMu.Lock()
	s.runs[record.ExternalID] = run
	s.runMu.Unlock()
	go s.executeRegistration(ctx, record, run)
	return run
}

func (s *localService) executeRegistration(ctx context.Context, record db.ChannelRegistrationRecord, run *registrationRun) {
	defer s.finishRun(record.ExternalID, run)
	options := &registration.Options{
		Source:    "open-managed-agents",
		AppPreset: &registration.AppPreset{Name: record.Name},
		Addons:    channelAppAddons(),
		OnQRCode: func(info *registration.QRCodeInfo) {
			record.URL = info.URL
			record.ExpiresAt = time.Now().Add(time.Duration(info.ExpireIn) * time.Second)
			s.persistRegistration(ctx, record)
			run.signal(registrationStart{URL: info.URL})
		},
		OnStatusChange: func(info *registration.StatusChangeInfo) {
			if info.Interval > 0 {
				record.IntervalSeconds = info.Interval
			}
			s.persistRegistration(ctx, record)
		},
	}
	result, err := s.registrar.RegisterApp(ctx, options)
	if ctx.Err() != nil {
		return
	}
	run.signal(registrationStart{Err: err})
	if err != nil {
		var denied *registration.AccessDeniedError
		var expired *registration.ExpiredError
		switch {
		case errors.As(err, &denied):
			s.saveRegistrationPhase(context.Background(), record, "denied")
		case errors.As(err, &expired):
			s.saveRegistrationPhase(context.Background(), record, "expired")
		default:
			s.saveRegistrationPhase(context.Background(), record, "unknown")
		}
		return
	}
	s.completeRegistration(context.Background(), record, result.ClientID, result.ClientSecret)
}

func (s *localService) finishRun(externalID string, run *registrationRun) {
	s.runMu.Lock()
	if current, ok := s.runs[externalID]; ok && current == run {
		delete(s.runs, externalID)
	}
	s.runMu.Unlock()
	run.cancel()
}

func (s *localService) activeRun(externalID string) *registrationRun {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.runs[externalID]
}

func (s *localService) cancelRun(externalID string) {
	s.runMu.Lock()
	run, ok := s.runs[externalID]
	delete(s.runs, externalID)
	s.runMu.Unlock()
	if ok {
		run.cancel()
	}
}

func (s *localService) stopRegistrations() {
	s.runMu.Lock()
	runs := make([]*registrationRun, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	s.runs = make(map[string]*registrationRun)
	s.runMu.Unlock()
	for _, run := range runs {
		run.cancel()
	}
}

func (s *localService) persistRegistration(ctx context.Context, record db.ChannelRegistrationRecord) {
	update := registrationUpdate(record, record.DeviceCodeEnvelope)
	if _, err := s.store.UpdateChannelRegistration(ctx, update); err != nil {
		s.logger.WarnContext(ctx, "update channel registration", "registration_id", record.ExternalID, "error", err)
	}
}

func channelAppAddons() *registration.AppAddons {
	return &registration.AppAddons{
		Scopes: registration.AppAddonsScopes{
			Tenant: []string{"im:message", "im:message:send_as_bot", "im:chat:readonly"},
		},
		Events: registration.AppAddonsEvents{
			Items: registration.AppAddonsEventItems{Tenant: []string{"im.message.receive_v1"}},
		},
	}
}
