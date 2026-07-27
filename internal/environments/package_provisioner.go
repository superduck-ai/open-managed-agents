package environments

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/samber/lo"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

const packageProvisionCommand = "/usr/local/bin/environment-manager provision-packages --protocol v1 --stdin"

type packageManifest struct {
	Version  int                 `json:"version"`
	Packages environmentPackages `json:"packages"`
}

type cloudEnvironmentPackagesConfig struct {
	Type     string               `json:"type"`
	Packages *environmentPackages `json:"packages"`
}

// packageProvisioningResult is the v1 JSON stdout contract from environment-manager.
// OMA only gates startup on status/exit agreement and optional failure diagnostics;
// category/stage/manager field matrices stay owned by environment-manager.
type packageProvisioningResult struct {
	Version      *int    `json:"version"`
	Status       *string `json:"status"`
	Category     *string `json:"category"`
	Manager      *string `json:"manager"`
	Stage        *string `json:"stage"`
	PackageCount *int    `json:"package_count"`
	DurationMS   *int64  `json:"duration_ms"`
	ExitCode     *int    `json:"exit_code"`
}

// buildPackageManifest 从已持久化的 Environment config 生成 provisioner manifest。
// 存量 config 已经过 HTTP 边界规范化，因此这里直接解码成命名 schema；仍然重新
// 校验一次，使被直接改写的数据库记录无法把非法 spec 送进 Sandbox。
func buildPackageManifest(config json.RawMessage) ([]byte, bool, error) {
	var cloud cloudEnvironmentPackagesConfig
	if err := json.Unmarshal(config, &cloud); err != nil {
		return nil, false, fmt.Errorf("decode environment config: %w", err)
	}
	if cloud.Type != "cloud" || cloud.Packages == nil {
		return nil, false, nil
	}
	packages := cloud.Packages
	if err := packages.validate(); err != nil {
		return nil, false, err
	}
	if packages.empty() {
		return nil, false, nil
	}
	data, err := json.Marshal(packageManifest{Version: 1, Packages: *packages.normalized()})
	if err != nil {
		return nil, false, fmt.Errorf("encode packages manifest: %w", err)
	}
	return data, true, nil
}

func validatePackageProvisioningResult(commandResult e2bruntime.CommandResult) error {
	result, err := decodePackageProvisioningResult(commandResult.Stdout)
	if err != nil {
		return err
	}
	if result.Version == nil || *result.Version != 1 || result.Status == nil || result.DurationMS == nil || *result.DurationMS < 0 {
		return errors.New("invalid package provisioning result: missing or invalid required field")
	}

	switch *result.Status {
	case "succeeded":
		if commandResult.ExitCode != 0 {
			return fmt.Errorf("invalid package provisioning result: succeeded status with process exit code %d", commandResult.ExitCode)
		}
		if result.PackageCount == nil || *result.PackageCount < 0 || hasPackageProvisioningFailureFields(result) {
			return errors.New("invalid package provisioning result: inconsistent succeeded fields")
		}
		return nil
	case "failed":
		if commandResult.ExitCode == 0 {
			return errors.New("invalid package provisioning result: failed status with process exit code 0")
		}
		return fmt.Errorf(
			"provision environment packages: status=failed category=%s manager=%s stage=%s package_count=%s duration_ms=%d exit_code=%s",
			lo.FromPtrOr(result.Category, "unknown"),
			lo.FromPtrOr(result.Manager, "unknown"),
			lo.FromPtrOr(result.Stage, "unknown"),
			optionalPackageProvisioningInt(result.PackageCount),
			*result.DurationMS,
			optionalPackageProvisioningInt(result.ExitCode),
		)
	default:
		return errors.New("invalid package provisioning result: unknown status")
	}
}

func decodePackageProvisioningResult(stdout []byte) (packageProvisioningResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	decoder.DisallowUnknownFields()
	var result packageProvisioningResult
	if err := decoder.Decode(&result); err != nil {
		return packageProvisioningResult{}, errors.New("invalid package provisioning result: decode failed")
	}
	if decoder.More() {
		return packageProvisioningResult{}, errors.New("invalid package provisioning result: multiple JSON values")
	}
	return result, nil
}

func hasPackageProvisioningFailureFields(result packageProvisioningResult) bool {
	return result.Category != nil || result.Manager != nil || result.Stage != nil || result.ExitCode != nil
}

func optionalPackageProvisioningInt(value *int) string {
	if value == nil {
		return "unknown"
	}
	return strconv.Itoa(*value)
}
