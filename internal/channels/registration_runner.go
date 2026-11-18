package channels

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	registrationStartTimeout = 15 * time.Second
	registrationDefaultTTL   = 10 * time.Minute
)

type appRegistrar = contracts.Registrar

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
	record.Owner = s.instanceID
	options := &contracts.RegistrationOptions{
		Name: record.Name,
		OnQRCode: func(url string, expiresIn int) {
			record.URL = url
			record.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
			s.persistRegistration(ctx, record)
			run.signal(registrationStart{URL: url})
		},
		OnInterval: func(interval int) {
			if interval > 0 {
				record.IntervalSeconds = interval
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
		switch {
		case errors.Is(err, contracts.ErrRegistrationDenied):
			s.saveRegistrationPhase(context.Background(), record, "denied")
		case errors.Is(err, contracts.ErrRegistrationExpired):
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
	if _, err := s.store.UpdateChannelRegistration(ctx, registrationUpdate(record, record.DeviceCodeEnvelope)); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.logger.WarnContext(ctx, "update channel registration", "registration_id", record.ExternalID, "error", err)
	}
}
