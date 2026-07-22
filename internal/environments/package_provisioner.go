package environments

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

const packageProvisionCommand = "/usr/local/bin/environment-manager provision-packages --protocol v1 --stdin"

var packageCredentialURLPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^/@\s]+@`)

type packageManifest struct {
	Version  int                 `json:"version"`
	Packages environmentPackages `json:"packages"`
}

type cloudEnvironmentPackagesConfig struct {
	Type     string               `json:"type"`
	Packages *environmentPackages `json:"packages"`
}

type packageProvisioningResult struct {
	Version      *int                         `json:"version"`
	Status       *string                      `json:"status"`
	Category     *packageProvisioningCategory `json:"category"`
	Manager      *string                      `json:"manager"`
	Stage        *string                      `json:"stage"`
	PackageCount *int                         `json:"package_count"`
	DurationMS   *int64                       `json:"duration_ms"`
	ExitCode     *int                         `json:"exit_code"`
}

type packageProvisioningCategory string

const (
	packageProvisioningInvalidManifest      packageProvisioningCategory = "invalid_manifest"
	packageProvisioningPackageManager       packageProvisioningCategory = "package_manager"
	packageProvisioningTimeout              packageProvisioningCategory = "timeout"
	packageProvisioningCancelled            packageProvisioningCategory = "cancelled"
	packageProvisioningExecutionEnvironment packageProvisioningCategory = "execution_environment"
	packageProvisioningInternal             packageProvisioningCategory = "internal"
)

type packageProvisioningCategoryContract struct {
	processExitCode int
	validFields     func(packageProvisioningResult) bool
}

var packageProvisioningCategoryContracts = map[packageProvisioningCategory]packageProvisioningCategoryContract{
	packageProvisioningInvalidManifest:      {processExitCode: 2, validFields: validInvalidManifestResult},
	packageProvisioningPackageManager:       {processExitCode: 10, validFields: validPackageManagerResult},
	packageProvisioningTimeout:              {processExitCode: 11, validFields: validInterruptedPackageProvisioningResult},
	packageProvisioningCancelled:            {processExitCode: 11, validFields: validInterruptedPackageProvisioningResult},
	packageProvisioningExecutionEnvironment: {processExitCode: 12, validFields: validExecutionEnvironmentResult},
	packageProvisioningInternal:             {processExitCode: 12, validFields: validInternalPackageProvisioningResult},
}

func buildPackageManifest(config json.RawMessage) ([]byte, bool, error) {
	var cloud cloudEnvironmentPackagesConfig
	if err := json.Unmarshal(config, &cloud); err != nil {
		return nil, false, fmt.Errorf("decode environment config: %w", err)
	}
	if cloud.Type != "cloud" {
		return nil, false, nil
	}
	packages := cloud.Packages
	if packages == nil {
		return nil, false, nil
	}
	packages.ensureLists()
	if packages.Type != "" && packages.Type != managerPackageType {
		return nil, false, errors.New(invalidPackagesTypeMessage)
	}
	if packages.hasCredentialURL() {
		return nil, false, errors.New("config.packages entries must not contain URL credentials")
	}
	if packages.hasManagerOption() {
		return nil, false, errors.New(invalidPackageOptionMessage)
	}
	if packages.empty() {
		return nil, false, nil
	}
	packages.Type = managerPackageType
	data, err := json.Marshal(packageManifest{Version: 1, Packages: *packages})
	if err != nil {
		return nil, false, fmt.Errorf("encode packages manifest: %w", err)
	}
	return data, true, nil
}

func (p *environmentPackages) empty() bool {
	return len(p.APT) == 0 && len(p.Cargo) == 0 && len(p.Gem) == 0 &&
		len(p.Go) == 0 && len(p.NPM) == 0 && len(p.PIP) == 0
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
		return validateFailedPackageProvisioningResult(commandResult.ExitCode, result)
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
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return packageProvisioningResult{}, errors.New("invalid package provisioning result: multiple JSON values")
	}
	return result, nil
}

func validateFailedPackageProvisioningResult(processExitCode int, result packageProvisioningResult) error {
	if result.Category == nil || result.Stage == nil {
		return errors.New("invalid package provisioning result: failed result is missing category or stage")
	}
	contract, valid := packageProvisioningCategoryContracts[*result.Category]
	if !valid {
		return errors.New("invalid package provisioning result: unknown category")
	}
	if processExitCode != contract.processExitCode {
		return fmt.Errorf("invalid package provisioning result: category and process exit code %d disagree", processExitCode)
	}
	if !contract.validFields(result) {
		return errors.New("invalid package provisioning result: inconsistent failure fields")
	}
	return fmt.Errorf(
		"provision environment packages: status=failed category=%s manager=%s stage=%s package_count=%s duration_ms=%d exit_code=%s",
		*result.Category,
		optionalPackageProvisioningString(result.Manager),
		*result.Stage,
		optionalPackageProvisioningInt(result.PackageCount),
		*result.DurationMS,
		optionalPackageProvisioningInt(result.ExitCode),
	)
}

func validInvalidManifestResult(result packageProvisioningResult) bool {
	return (*result.Stage == "decode" || *result.Stage == "validate") &&
		result.Manager == nil && result.PackageCount == nil && result.ExitCode == nil
}

func validPackageManagerResult(result packageProvisioningResult) bool {
	return validPackageProvisioningManager(result.Manager) && validPackageProvisioningCount(result.PackageCount) &&
		packageProvisioningManagerStageValid(*result.Stage, result.Manager) &&
		result.ExitCode != nil && *result.ExitCode > 0
}

func validInterruptedPackageProvisioningResult(result packageProvisioningResult) bool {
	return validOptionalPackageProvisioningManager(result.Manager) && validPackageProvisioningCount(result.PackageCount) &&
		packageProvisioningActiveStageValid(*result.Stage, result.Manager) && result.ExitCode == nil
}

func validExecutionEnvironmentResult(result packageProvisioningResult) bool {
	if !validPackageProvisioningCount(result.PackageCount) || result.ExitCode != nil {
		return false
	}
	return (*result.Stage == "preflight" && result.Manager == nil) ||
		(*result.Stage == "start" && validPackageProvisioningManager(result.Manager))
}

func validInternalPackageProvisioningResult(result packageProvisioningResult) bool {
	if result.ExitCode != nil {
		return false
	}
	switch *result.Stage {
	case "finalize":
		return result.Manager == nil && (result.PackageCount == nil || validPackageProvisioningCount(result.PackageCount))
	case "preflight":
		return result.Manager == nil && validPackageProvisioningCount(result.PackageCount)
	case "prepare", "install":
		return validPackageProvisioningCount(result.PackageCount) && packageProvisioningManagerStageValid(*result.Stage, result.Manager)
	default:
		return false
	}
}

func validOptionalPackageProvisioningManager(manager *string) bool {
	return manager == nil || packageProvisioningManagerValid(*manager)
}

func validPackageProvisioningManager(manager *string) bool {
	return manager != nil && packageProvisioningManagerValid(*manager)
}

func validPackageProvisioningCount(count *int) bool {
	return count != nil && *count >= 0
}

func packageProvisioningActiveStageValid(stage string, manager *string) bool {
	if stage == "preflight" {
		return manager == nil
	}
	return packageProvisioningManagerStageValid(stage, manager)
}

func packageProvisioningManagerStageValid(stage string, manager *string) bool {
	if !validPackageProvisioningManager(manager) {
		return false
	}
	return stage == "install" || (stage == "prepare" && *manager == "apt")
}

func packageProvisioningManagerValid(manager string) bool {
	switch manager {
	case "apt", "cargo", "gem", "go", "npm", "pip":
		return true
	default:
		return false
	}
}

func hasPackageProvisioningFailureFields(result packageProvisioningResult) bool {
	return result.Category != nil || result.Manager != nil || result.Stage != nil || result.ExitCode != nil
}

func optionalPackageProvisioningString(value *string) string {
	if value == nil {
		return "unknown"
	}
	return *value
}

func optionalPackageProvisioningInt(value *int) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *value)
}
