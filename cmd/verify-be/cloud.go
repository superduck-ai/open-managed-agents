package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

type cloudConfig struct {
	Purpose  string          `yaml:"purpose"`
	Storage  config.S3Config `yaml:"storage"`
	ReadOnly struct {
		AccessKeyID     string `yaml:"access_key_id"`
		SecretAccessKey string `yaml:"secret_access_key"`
	} `yaml:"read_only"`
	E2B config.E2BConfig `yaml:"e2b"`
}

type cloudResources struct {
	ObjectKey string `json:"object_key,omitempty"`
	SandboxID string `json:"sandbox_id,omitempty"`
}

func isCloudScenario(name string) bool {
	return name == "files.cloud-storage" || name == "chat.cloud-renewal"
}

func loadCloudConfig(options cliOptions) (cloudConfig, error) {
	var cfg cloudConfig
	path := strings.TrimSpace(options.CloudConfig)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("VERIFY_BE_CLOUD_CONFIG"))
	}
	if path == "" {
		return cfg, errors.New("cloud verification requires --cloud-config or VERIFY_BE_CLOUD_CONFIG; no cloud resources were created")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 65536 {
		return cfg, errors.New("cloud config must be a readable private regular file, mode 0600 or 0400, at most 64 KiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return cfg, errors.New("cannot read cloud config")
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, errors.New("invalid cloud config YAML or unknown fields")
	}
	if cfg.Purpose != "verify-be" {
		return cfg, errors.New("cloud config must declare purpose: verify-be for a dedicated test environment")
	}
	if options.Scenario == "files.cloud-storage" {
		if cfg.Storage.Bucket == "" || cfg.Storage.Region == "" || cfg.Storage.AccessKeyID == "" || cfg.Storage.SecretAccessKey == "" || cfg.ReadOnly.AccessKeyID == "" || cfg.ReadOnly.SecretAccessKey == "" || cfg.Storage.AccessKeyID == cfg.ReadOnly.AccessKeyID {
			return cfg, errors.New("cloud storage requires bucket, region, owner credentials and distinct read-only credentials")
		}
		if cfg.Storage.Endpoint == "" || !cloudEndpoint(cfg.Storage.Endpoint) {
			return cfg, errors.New("cloud storage endpoint must be HTTPS without userinfo, query or fragment")
		}
	} else {
		if cfg.E2B.APIKey == "" || cfg.E2B.Debug || cfg.E2B.Template == "" {
			return cfg, errors.New("cloud renewal requires E2B API key, template and debug=false")
		}
		if !cloudEndpoint(cfg.E2B.APIURL) || !cloudEndpoint(cfg.E2B.SandboxURL) {
			return cfg, errors.New("cloud E2B endpoints must be HTTPS without userinfo, query or fragment")
		}
		cfg.E2B.RequestTimeout = 30 * time.Second
		cfg.E2B.SandboxTimeout = time.Minute
	}
	return cfg, nil
}

func cloudEndpoint(endpoint string) bool {
	if endpoint == "" {
		return true
	}
	parsed, err := url.Parse(endpoint)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func performCloud(ctx context.Context, root, directory string, options cliOptions, selected scenario, r *report) error {
	cfg, err := loadCloudConfig(options)
	if err != nil {
		r.Status, r.FailureKind, r.CleanupComplete = "blocked", "prerequisite", true
		return err
	}
	r.Doctor = doctorResult{Machine: runtime.GOOS + "/" + runtime.GOARCH, Versions: map[string]string{"go": runtime.Version()}}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	r.BinarySHA256, err = fileSHA256(executable)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, selected.Timeout)
	defer cancel()
	started := time.Now()
	stage := func(name string) {
		r.Result.Timeline = append(r.Result.Timeline, proof{Stage: name, ElapsedMS: time.Since(started).Milliseconds()})
	}
	r.CleanupComplete = true
	if r.Scenario == "files.cloud-storage" {
		err = verifyCloudStorage(ctx, cfg, directory, r, stage)
	} else {
		err = verifyCloudRenewal(ctx, cfg, directory, r, stage)
	}
	if err != nil {
		return err
	}
	if len(r.Result.Timeline) != len(selected.Stages) {
		return errors.New("incomplete cloud assertions")
	}
	for i, expected := range selected.Stages {
		if r.Result.Timeline[i].Stage != expected {
			return errors.New("missing cloud proof stage")
		}
	}
	after, err := sourceIdentity(ctx, root)
	if err != nil {
		return err
	}
	if after != r.Source {
		return errors.New("source changed during cloud verification")
	}
	r.Status, r.Result.Status = "pass", "pass"
	return nil
}

func saveCloudResources(directory string, resources cloudResources) error {
	return writeJSON(filepath.Join(directory, "cloud-resources.json"), resources)
}

func cloudCleanup(r *report, err error) {
	if err != nil {
		r.CleanupComplete = false
		r.CleanupErrors = append(r.CleanupErrors, "cloud cleanup failed; use cloud-resources.json and the original private configuration to reclaim owned resources")
	}
}
