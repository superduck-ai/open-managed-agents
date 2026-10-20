package devinit

import (
	"context"
	"fmt"
)

// SetRuntime rewrites the generated config with freshly probed listener
// ports and records them in state so the next restart knows what to stop.
// Either port may be zero to keep the previous value.
func SetRuntime(ctx context.Context, repoRoot string, apiPort, webPort int) error {
	repoRoot, err := filepathEvalSymlinks(repoRoot)
	if err != nil {
		return err
	}
	unlock, err := acquireInitLock(generatedDir(repoRoot))
	if err != nil {
		return err
	}
	defer unlock()
	return setRuntimeLocked(ctx, repoRoot, apiPort, webPort)
}

func setRuntimeLocked(ctx context.Context, repoRoot string, apiPort, webPort int) error {
	binding, found, err := LoadBinding(envFilePath(repoRoot))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("environment is not bound (no .run/env.json)")
	}
	base, err := loadBaseConfig(repoRoot)
	if err != nil {
		return err
	}
	layout, err := NewLayout(binding, base, repoRoot)
	if err != nil {
		return err
	}
	state, found, err := LoadState(layout.StateFile)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("environment state %s is missing; rerun dev-init", layout.StateFile)
	}
	runtime := RuntimePorts{APIPort: apiPort, WebPort: webPort}
	if state.Runtime != nil {
		if runtime.APIPort == 0 {
			runtime.APIPort = state.Runtime.APIPort
		}
		if runtime.WebPort == 0 {
			runtime.WebPort = state.Runtime.WebPort
		}
		runtime.RedisPort = state.Runtime.RedisPort
	}
	if runtime.APIPort == 0 {
		if runtime.APIPort, err = ProbeFreePort(apiPortScanBase); err != nil {
			return err
		}
	}
	if runtime.WebPort == 0 {
		if runtime.WebPort, err = ProbeFreePort(webPortScanBase); err != nil {
			return err
		}
	}
	stackPorts, err := EnsureStack(ctx, repoRoot, binding, &runtime)
	if err != nil {
		return err
	}
	runtime.PGPort = stackPorts.PGPort
	runtime.RedisPort = stackPorts.RedisPort
	runtime.NATSPort = stackPorts.NATSPort
	runtime.NATSPort2 = stackPorts.NATSPort2
	runtime.NATSPort3 = stackPorts.NATSPort3
	runtime.MinIOPort = stackPorts.MinIOPort
	runtime.E2BPort = stackPorts.E2BPort
	spec, err := LoadSpec(binding.InitFile)
	if err != nil {
		return err
	}
	credentials, found, err := LoadCredentials(layout.CredentialsFile)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("credentials %s are missing; cannot regenerate the environment config", layout.CredentialsFile)
	}
	if err := GenerateConfig(baseConfigPath(repoRoot), layout, base, spec, state, credentials, runtime); err != nil {
		return err
	}
	state.Runtime = &runtime
	return SaveState(layout, state)
}

func ensureListenerPorts(runtime *RuntimePorts) error {
	var err error
	if runtime.APIPort == 0 {
		runtime.APIPort, err = ProbeFreePort(apiPortScanBase)
		if err != nil {
			return err
		}
	}
	if runtime.WebPort == 0 {
		runtime.WebPort, err = ProbeFreePort(webPortScanBase)
	}
	return err
}
