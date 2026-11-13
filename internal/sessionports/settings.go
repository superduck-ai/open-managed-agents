package sessionports

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	envdPort = 49983
	ToolName = "get_port_url"
)

type Address struct {
	Port        int
	SessionUUID string
}

type Settings struct {
	domainSuffix string
	urlScheme    string
	urlPort      int
	blocked      map[int]struct{}
	codec        *LabelCodec
}

func New(cfg config.Config, logger *slog.Logger) (*Settings, error) {
	ports := cfg.SessionPorts
	if !ports.Enabled() {
		return &Settings{}, nil
	}
	key, err := labelKey(cfg, logging.LoggerOrDefault(logger))
	if err != nil {
		return nil, err
	}
	codec, err := NewLabelCodec(key)
	if err != nil {
		return nil, err
	}
	return NewWithCodec(ports, cfg.EnvironmentRunner.EffectiveSandboxMCPPort(), codec), nil
}

func NewWithCodec(ports config.SessionPortsConfig, sandboxMCPPort int, codec *LabelCodec) *Settings {
	blocked := map[int]struct{}{envdPort: {}}
	if sandboxMCPPort > 0 {
		blocked[sandboxMCPPort] = struct{}{}
	}
	for _, port := range ports.BlockedPorts {
		blocked[port] = struct{}{}
	}
	return &Settings{
		domainSuffix: ports.DomainSuffix,
		urlScheme:    ports.URLScheme,
		urlPort:      ports.URLPort,
		blocked:      blocked,
		codec:        codec,
	}
}

func labelKey(cfg config.Config, logger *slog.Logger) ([]byte, error) {
	if path := strings.TrimSpace(cfg.SessionPorts.LabelKeyFile); path != "" {
		return LoadLabelKey(path)
	}
	logger.Warn("session port label key file is empty; using an ephemeral key that changes on restart")
	return ephemeralLabelKey()
}

func (s *Settings) Enabled() bool {
	return s != nil && s.codec != nil
}

func (s *Settings) ForwardKey() []byte {
	if !s.Enabled() {
		return nil
	}
	return s.codec.ForwardKey()
}

func (s *Settings) PortAllowed(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	_, blocked := s.blocked[port]
	return !blocked
}

func (s *Settings) URL(sessionUUID string, port int) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	if !s.PortAllowed(port) {
		return "", ErrPortBlocked
	}
	label, err := s.codec.Encode(sessionUUID, port)
	if err != nil {
		return "", err
	}
	host := strconv.Itoa(port) + "-" + label + "." + s.domainSuffix
	if s.urlPort > 0 {
		host = net.JoinHostPort(host, strconv.Itoa(s.urlPort))
	}
	return fmt.Sprintf("%s://%s/", s.urlScheme, host), nil
}

func (s *Settings) MatchesHost(host string) bool {
	if !s.Enabled() {
		return false
	}
	return strings.HasSuffix(normalizeHost(host), "."+s.domainSuffix)
}

func (s *Settings) ParseHost(host string) (Address, error) {
	if !s.MatchesHost(host) {
		return Address{}, ErrInvalidHost
	}
	hostLabel, ok := strings.CutSuffix(normalizeHost(host), "."+s.domainSuffix)
	if !ok || strings.Contains(hostLabel, ".") {
		return Address{}, ErrInvalidHost
	}
	portText, label, ok := strings.Cut(hostLabel, "-")
	if !ok {
		return Address{}, ErrInvalidHost
	}
	port, err := strconv.Atoi(portText)
	if err != nil || strconv.Itoa(port) != portText {
		return Address{}, ErrInvalidHost
	}
	if !s.PortAllowed(port) {
		return Address{}, ErrPortBlocked
	}
	sessionUUID, err := s.codec.Decode(label, port)
	if err != nil {
		return Address{}, err
	}
	return Address{Port: port, SessionUUID: sessionUUID}, nil
}

func normalizeHost(host string) string {
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
