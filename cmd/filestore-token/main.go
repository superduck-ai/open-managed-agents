// filestore-token 为联调环境签发 Filestore JWT。
//
// 此命令直接读取服务端签名私钥，必须只在受信环境中运行；输出的 token
// 也应按凭证管理，避免写入 shell 历史、日志或版本库。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

type tokenIssuer interface {
	Issue(identity filestore.TokenIdentity) (string, error)
	IssueReadonly(identity filestore.TokenIdentity) (string, error)
}

type issuerLoader func(configPath string) (tokenIssuer, error)

type options struct {
	configPath                string
	subject                   string
	orgUUID                   string
	accountUUID               string
	workspaceUUID             string
	workspaceTaggedID         string
	resolvedWorkspaceTaggedID string
	filesystemID              string
	orgTaints                 stringList
	workspaceCMEKEnabled      bool
	readonly                  bool
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, loadIssuer))
}

func run(args []string, stdout, stderr io.Writer, load issuerLoader) int {
	opts, err := parseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	if err := opts.validate(); err != nil {
		fmt.Fprintf(stderr, "filestore-token: %v\n", err)
		return 2
	}

	issuer, err := load(opts.configPath)
	if err != nil {
		fmt.Fprintf(stderr, "filestore-token: load signing credentials: %v\n", err)
		return 1
	}
	identity := filestore.TokenIdentity{
		Subject:                   opts.subject,
		OrgUUID:                   opts.orgUUID,
		AccountUUID:               opts.accountUUID,
		WorkspaceUUID:             opts.workspaceUUID,
		WorkspaceTaggedID:         opts.workspaceTaggedID,
		ResolvedWorkspaceTaggedID: opts.resolvedWorkspaceTaggedID,
		FilesystemID:              opts.filesystemID,
		OrgTaints:                 []string(opts.orgTaints),
		WorkspaceCMEKEnabled:      opts.workspaceCMEKEnabled,
	}

	var token string
	if opts.readonly {
		token, err = issuer.IssueReadonly(identity)
	} else {
		token, err = issuer.Issue(identity)
	}
	if err != nil {
		fmt.Fprintf(stderr, "filestore-token: issue token: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, token); err != nil {
		fmt.Fprintf(stderr, "filestore-token: write token: %v\n", err)
		return 1
	}
	return 0
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("filestore-token", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.configPath, "config", "", "path to server config; defaults to CONFIG_FILE or config/config.yaml discovery")
	flags.StringVar(&opts.subject, "sub", "", "subject identifier")
	flags.StringVar(&opts.orgUUID, "org-uuid", "", "organization UUID")
	flags.StringVar(&opts.accountUUID, "account-uuid", "", "account UUID")
	flags.StringVar(&opts.workspaceUUID, "workspace-uuid", "", "workspace UUID")
	flags.StringVar(&opts.workspaceTaggedID, "workspace-tagged-id", "", "workspace tagged ID")
	flags.StringVar(&opts.resolvedWorkspaceTaggedID, "resolved-workspace-tagged-id", "", "resolved workspace tagged ID; defaults to --workspace-tagged-id")
	flags.StringVar(&opts.filesystemID, "filesystem-id", "", "existing Filestore filesystem external ID or UUID")
	flags.Var(&opts.orgTaints, "org-taint", "organization taint; repeat the flag for multiple values")
	flags.BoolVar(&opts.workspaceCMEKEnabled, "workspace-cmek-enabled", false, "set workspace_cmek_enabled to true")
	flags.BoolVar(&opts.readonly, "readonly", false, "issue Token 2 with readonly=true")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(opts.resolvedWorkspaceTaggedID) == "" {
		opts.resolvedWorkspaceTaggedID = opts.workspaceTaggedID
	}
	return opts, nil
}

func (opts options) validate() error {
	required := []struct {
		name  string
		value string
	}{
		{name: "sub", value: opts.subject},
		{name: "org-uuid", value: opts.orgUUID},
		{name: "account-uuid", value: opts.accountUUID},
		{name: "workspace-uuid", value: opts.workspaceUUID},
		{name: "workspace-tagged-id", value: opts.workspaceTaggedID},
		{name: "resolved-workspace-tagged-id", value: opts.resolvedWorkspaceTaggedID},
		{name: "filesystem-id", value: opts.filesystemID},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("--%s is required", field.name)
		}
	}
	return nil
}

func loadIssuer(configPath string) (tokenIssuer, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return filestore.NewTokenCredentials(cfg)
}

func loadConfig(configPath string) (config.Config, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return config.Load()
	}

	previous, existed := os.LookupEnv("CONFIG_FILE")
	if err := os.Setenv("CONFIG_FILE", configPath); err != nil {
		return config.Config{}, err
	}
	defer func() {
		if existed {
			_ = os.Setenv("CONFIG_FILE", previous)
		} else {
			_ = os.Unsetenv("CONFIG_FILE")
		}
	}()
	return config.Load()
}
