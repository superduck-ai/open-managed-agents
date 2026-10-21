package devinit

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func GenerateConfig(baseConfigPath string, layout Layout, base config.Config, spec Spec, state State, runtime RuntimePorts) error {
	if runtime.APIPort < 1 {
		return fmt.Errorf("runtime api port is required")
	}
	data, err := os.ReadFile(baseConfigPath)
	if err != nil {
		return fmt.Errorf("read base config: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse base config: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("base config %s must be a single YAML mapping", baseConfigPath)
	}
	root := document.Content[0]

	apiAddr := "127.0.0.1:" + strconv.Itoa(runtime.APIPort)
	setMappingValue(root, "server", "addr", apiAddr)
	parsedDB, err := replaceURLPort(base.Database.URL, runtime.PGPort)
	if err != nil {
		return err
	}
	setMappingValue(root, "database", "url", parsedDB)
	redisURL, err := redisURLWithPort(base.Redis.URL, runtime.RedisPort)
	if err != nil {
		return err
	}
	setMappingValue(root, "redis", "url", redisURL)
	natsURL := fmt.Sprintf("nats://127.0.0.1:%d,nats://127.0.0.1:%d,nats://127.0.0.1:%d", runtime.NATSPort, runtime.NATSPort2, runtime.NATSPort3)
	setMappingValue(root, "nats", "url", natsURL)
	setMappingValue(root, "storage", "s3", "bucket", base.Storage.S3.Bucket)
	endpoint, err := replaceURLPort(base.Storage.S3.Endpoint, runtime.MinIOPort)
	if err != nil {
		return err
	}
	setMappingValue(root, "storage", "s3", "endpoint", endpoint)
	e2bURL := "http://127.0.0.1:" + strconv.Itoa(runtime.E2BPort)
	setMappingValue(root, "e2b", "api_url", e2bURL)
	setMappingValue(root, "e2b", "sandbox_url", e2bURL)
	setMappingValue(root, "e2b", "local_service_host", "127.0.0.1")
	apiKey := base.E2B.APIKey
	if apiKey == "" {
		apiKey = "e2b_0000000000000000000000000000000000000000"
	}
	setMappingValue(root, "e2b", "api_key", apiKey)
	setMappingValue(root, "e2b", "template", localSandboxTemplate(base.E2B.Template))
	setNode(getOrCreateMapping(root, "e2b"), "local_port_lookup", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	setMappingValue(root, "code_session", "sandbox_api_base_url", "http://host.docker.internal:"+strconv.Itoa(runtime.APIPort))
	setMappingValue(root, "tunnel", "public_base_url", "http://"+apiAddr)
	setMappingValue(root, "auth", "console_url", layout.WebPublicURL)

	absolutizeConfigPaths(root, base)

	setMappingValue(root, "code_session", "brain_claim_token_file", filepath.Join(layout.SecretsDir, "brain-claim-token"))
	setMappingValue(root, "code_session", "jwt_signing_private_key_file", filepath.Join(layout.SecretsDir, "code-session-jwt-ed25519.pem"))
	setMappingValue(root, "code_session", "upstream_proxy_ca_key_file", filepath.Join(layout.SecretsDir, "upstream-proxy-ca-key.pem"))

	defaultWorkspace, err := defaultWorkspaceState(state)
	if err != nil {
		return err
	}
	bootstrap := getOrCreateMapping(root, "bootstrap")
	setScalar(bootstrap, "organization_name", spec.OrganizationName)
	setScalar(bootstrap, "workspace_name", defaultWorkspace.Name)
	setScalar(bootstrap, "workspace_external_id", defaultWorkspace.ExternalID)
	setScalar(bootstrap, "user_external_id", state.User.ExternalID)
	setScalar(bootstrap, "user_email", state.User.Email)
	setScalar(bootstrap, "user_name", state.User.Name)
	setNode(bootstrap, "seed_api_keys", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})

	var output strings.Builder
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(layout.GeneratedConfig), 0o700); err != nil {
		return err
	}
	return os.WriteFile(layout.GeneratedConfig, []byte(output.String()), 0o600)
}

func defaultWorkspaceState(state State) (WorkspaceState, error) {
	for _, workspace := range state.Workspaces {
		if workspace.IsDefault {
			return workspace, nil
		}
	}
	return WorkspaceState{}, fmt.Errorf("state has no default workspace")
}

// absolutizeConfigPaths pins every runtime-resolved relative path field from
// the base config into the generated document as an absolute path.
func absolutizeConfigPaths(root *yaml.Node, base config.Config) {
	setPathIfPresent(root, []string{"code_session", "jwt_signing_private_key_file"}, base.CodeSession.JWTSigningPrivateKeyFile)
	setPathIfPresent(root, []string{"code_session", "upstream_proxy_ca_key_file"}, base.CodeSession.UpstreamProxyCAKeyFile)
	setPathIfPresent(root, []string{"code_session", "brain_claim_token_file"}, base.CodeSession.BrainClaimTokenFile)
	if local := base.Vault.MasterKey.Local; local != nil && local.KekFile != "" {
		setPathIfPresent(root, []string{"vault", "master_key", "local", "kek_file"}, local.KekFile)
	}
	if vault := base.Vault.MasterKey.HashicorpVault; vault != nil {
		setPathIfPresent(root, []string{"vault", "master_key", "hashicorp_vault", "token_file"}, vault.TokenFile)
		setPathIfPresent(root, []string{"vault", "master_key", "hashicorp_vault", "ca_file"}, vault.CAFile)
	}
}

func setPathIfPresent(root *yaml.Node, path []string, value string) {
	if value == "" {
		return
	}
	parent := root
	for _, key := range path[:len(path)-1] {
		parent = findMappingValue(parent, key)
		if parent == nil || parent.Kind != yaml.MappingNode {
			return
		}
	}
	setScalar(parent, path[len(path)-1], value)
}

// replaceURLPort swaps the port of an absolute URL, preserving scheme,
// credentials, path and query.
func replaceURLPort(rawURL string, port int) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("must be an absolute URL: %q", rawURL)
	}
	parsed.Host = parsed.Hostname() + ":" + strconv.Itoa(port)
	return parsed.String(), nil
}

func setMappingValue(root *yaml.Node, keysAndValue ...string) {
	if len(keysAndValue) < 2 {
		return
	}
	keys, value := keysAndValue[:len(keysAndValue)-1], keysAndValue[len(keysAndValue)-1]
	parent := root
	if len(keys) > 1 {
		parent = getOrCreateMapping(root, keys[:len(keys)-1]...)
	}
	setScalar(parent, keys[len(keys)-1], value)
}

func getOrCreateMapping(root *yaml.Node, keys ...string) *yaml.Node {
	current := root
	for _, key := range keys {
		value := findMappingValue(current, key)
		if value == nil {
			value = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			current.Content = append(current.Content, scalarKey(key), value)
		}
		current = value
	}
	return current
}

func setScalar(mapping *yaml.Node, key, value string) {
	setNode(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setNode(mapping *yaml.Node, key string, value *yaml.Node) {
	existing := findMappingValue(mapping, key)
	if existing != nil {
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value == key {
				mapping.Content[i+1] = value
				return
			}
		}
	}
	mapping.Content = append(mapping.Content, scalarKey(key), value)
}

func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func scalarKey(key string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
}
