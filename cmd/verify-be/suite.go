package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

func selectScenario(name string) scenario {
	if name == "test" {
		return scenario{Package: "./...", Timeout: 15 * time.Minute, Description: "全仓默认构建 Go 测试，隔离依赖并启用本地 MinIO 与迁移集成；逐项列出未验证测试"}
	}
	return scenarios[name]
}

func suiteEnvironment(e *environment) ([]string, error) {
	path := filepath.Join(e.directory, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	redisURL, err := url.Parse(cfg.Redis.URL)
	if err != nil {
		return nil, err
	}
	values := isolatedSuiteEnvironment(os.Environ())
	s3 := cfg.Storage.S3
	return append(values,
		"CONFIG_FILE="+path,
		"TEST_MIGRATION_DATABASE_URL="+cfg.Database.URL,
		"TEST_EVENT_PAYLOAD_S3=1",
		"REDIS_URL="+cfg.Redis.URL,
		"TEST_TUNNEL_REDIS_ADDR="+redisURL.Host,
		"OMA_S3_INTEGRATION_ENDPOINT="+s3.Endpoint,
		"OMA_S3_INTEGRATION_REGION="+s3.Region,
		"OMA_S3_INTEGRATION_BUCKET=oma-storage-test-"+e.runID,
		"OMA_S3_INTEGRATION_ACCESS_KEY_ID="+s3.AccessKeyID,
		"OMA_S3_INTEGRATION_SECRET_ACCESS_KEY="+s3.SecretAccessKey,
		"OMA_S3_INTEGRATION_DELETE_BUCKET=1",
	), nil
}

func isolatedSuiteEnvironment(values []string) []string {
	return slices.DeleteFunc(slices.Clone(values), func(value string) bool {
		name, _, _ := strings.Cut(value, "=")
		return name == "CONFIG_FILE" || name == "REDIS_URL" || name == "OMA_WORKER_CONTROL_IMAGE" || strings.HasPrefix(name, "LIVE_") || strings.HasPrefix(name, "VERIFY_BE_") || strings.HasPrefix(name, "TEST_") || strings.HasPrefix(name, "OMA_TEST_") || strings.HasPrefix(name, "OMA_S3_INTEGRATION_")
	})
}

func evaluateSuite(reader io.Reader, code int) (testResult, error) {
	result := testResult{Status: "fail", FailureKind: "test_failure", Returncode: code, Tests: map[string]string{}, Skipped: []string{}}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	failed := code != 0
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return result, errors.New("invalid Go suite JSON")
		}
		name := event.Package
		if event.Test != "" {
			name += "/" + event.Test
		}
		switch event.Action {
		case "start":
			result.Tests[name] = "running"
		case "pass", "fail", "skip":
			result.Tests[name] = event.Action
		}
		if event.Action == "fail" {
			failed = true
		}
		if event.Action == "skip" && event.Test != "" {
			result.Skipped = append(result.Skipped, name)
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	for _, status := range result.Tests {
		failed = failed || status == "running"
	}
	for _, required := range []string{
		"internal/db/TestSessionInputIndexMigration",
		"internal/storage/TestS3CompatibleIntegration",
		"internal/platformauth/TestRedisEmailCodeStoreIntegration",
		"internal/tunnels/TestPresenceRedis8",
		"internal/tunnels/TestRequestBindingsRedis8",
		"internal/tunnels/TestRequestBindingsRedis8CrossInstanceResponse",
		"tests/TestEventPayloadIntegrationRealS3",
		"tests",
	} {
		if result.Tests["github.com/superduck-ai/open-managed-agents/"+required] != "pass" {
			result.MissingStages = append(result.MissingStages, required)
			failed = true
		}
	}
	slices.Sort(result.Skipped)
	if !failed {
		result.Status, result.FailureKind = "pass", ""
		if len(result.Skipped) != 0 {
			result.Status = "passed_with_skips"
		}
	}
	return result, nil
}
