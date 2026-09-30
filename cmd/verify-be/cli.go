package main

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type cliOptions struct {
	CloudConfig string
	Command     string
	Scenario    string
	WorkerImage string
	Baseline    string
	BackendRef  string
	Diagnostics bool
	Timeout     time.Duration
}

var errHelp = errors.New("help requested")

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	var options cliOptions
	root := newCLI(&options)
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(args)
	var helpResult error
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(command *cobra.Command, args []string) {
		if helpResult = command.ValidateArgs(command.Flags().Args()); helpResult != nil {
			return
		}
		defaultHelp(command, args)
		helpResult = errHelp
	})
	err := root.Execute()
	if err == nil {
		err = helpResult
	}
	return options, err
}

func newCLI(options *cliOptions) *cobra.Command {
	root := &cobra.Command{
		Use: "verify-be", Short: "后端自验证：隔离环境、客观断言、证据报告和清理",
		Long:          "后端自验证工具。chat 使用真实 Worker 与固定模型；files 使用实际后端；transcript 复用生产归档服务和维护 CLI；memory 验证 Memory/Filestore 与实际 FUSE 挂载。\n本地依赖使用隔离 PostgreSQL 与 MinIO。报告: tmp/verify-be/<run-id>/report.json 和 report.md\n退出码: 0 = 通过或帮助；1 = 失败；2 = 参数错误或先决条件阻塞。",
		SilenceErrors: true, SilenceUsage: true, Args: cobra.NoArgs,
		RunE:    missingCommand,
		Example: "  just verify-be files doctor\n  just verify-be files lifecycle\n  just verify-be chat roundtrip\n  just verify-be chat performance --timeout 8m",
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().DurationVar(&options.Timeout, "timeout", 0, "覆盖场景执行期限，例如 5m；不含构建和清理")
	root.AddCommand(doctorCommand(options, ""))
	root.AddCommand(&cobra.Command{
		Use: "test", Short: "在临时 PostgreSQL、Redis、NATS 和 MinIO 中运行全仓 Go 测试",
		Long: "运行 go test ./... -json -count=1，启用本地 S3 和迁移集成测试。\n默认期限 15m；报告逐项列出失败与跳过。云端与独立 live 场景不算通过。",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			options.Command, options.Scenario = "run", "test"
			return validateOptions(*options)
		},
	})
	for _, domain := range []string{"chat", "files", "transcript", "memory"} {
		group := &cobra.Command{Use: domain, Short: domain + " 场景", Args: cobra.NoArgs, RunE: missingCommand}
		if domain == "chat" {
			group.Long = "聊天验证使用真实 Worker 和固定模型。\n镜像优先级: --worker-image > OMA_WORKER_CONTROL_IMAGE > .verify-be.local.json 的 worker_image > 公共默认镜像。\n本地配置被 Git 忽略；帮助不显示私有值。公共默认镜像: " + defaultWorker
			group.PersistentFlags().StringVar(&options.WorkerImage, "worker-image", "", "覆盖 Worker 镜像，不自动拉取")
		} else if domain == "files" {
			group.Long = "Files API 与真实对象存储验证。generated 额外需要 Worker 镜像和 FUSE；其他场景无需 Worker。"
		} else if domain == "memory" {
			group.Long = "Memory / Filestore：真实 PostgreSQL/MinIO 验证读写、隔离、跨会话生命周期和清理。mounts 使用实际 Runner 与 Docker/FUSE；其他场景不需要 Worker。"
		} else {
			group.Long = "Transcript 私有历史归档集成验证。复用生产服务和真实 PostgreSQL/MinIO；lifecycle 运行实际导出/还原 CLI。\n不启动独立后端或 Worker，不验证 River 定时 sweep；无需 Worker 镜像与 FUSE。"
		}
		group.AddCommand(doctorCommand(options, domain))
		for name, selected := range scenarios {
			prefix, short, _ := strings.Cut(name, ".")
			if prefix != domain {
				continue
			}
			command := &cobra.Command{
				Use: short, Short: selected.Description,
				Long: selected.Description + "\n默认场景期限: " + selected.Timeout.String() + "\n报告: tmp/verify-be/<run-id>/report.json 和 report.md",
				Args: cobra.NoArgs,
				RunE: func(_ *cobra.Command, _ []string) error {
					options.Command, options.Scenario = "run", name
					return validateOptions(*options)
				},
			}
			if name == "files.generated" || name == "memory.mounts" {
				command.Flags().StringVar(&options.WorkerImage, "worker-image", "", "覆盖 Worker 镜像，不自动拉取")
			}
			if isCloudScenario(name) {
				command.Flags().StringVar(&options.CloudConfig, "cloud-config", "", "专用云测试 YAML 配置路径；默认读取 VERIFY_BE_CLOUD_CONFIG，不打印凭据")
			}
			if strings.HasSuffix(name, ".performance") {
				flags := command.Flags()
				flags.StringVar(&options.Baseline, "baseline", "", "与通过的 report.json 比较，退化则失败")
				flags.StringVar(&options.BackendRef, "backend-ref", "", "构建指定 Git 提交的后端，使用当前验证负载")
				flags.BoolVar(&options.Diagnostics, "diagnostics", false, "采集 CPU、heap、trace；不参与基线比较")
			}
			group.AddCommand(command)
		}
		root.AddCommand(group)
	}
	root.SetHelpCommand(&cobra.Command{Use: "help [命令...]", RunE: func(command *cobra.Command, args []string) error {
		target, remaining, err := root.Find(args)
		if err != nil {
			return err
		}
		if target == command || len(remaining) != 0 {
			return errors.New("unknown help topic")
		}
		return target.Help()
	}})
	return root
}

func missingCommand(command *cobra.Command, _ []string) error {
	return errors.New("missing command; use " + command.CommandPath() + " -h")
}

func doctorCommand(options *cliOptions, domain string) *cobra.Command {
	use := "doctor"
	if domain != "" {
		use += " [场景]"
	}
	command := &cobra.Command{
		Use: use, Short: "检查工具、本地镜像、18080 端口和 Docker 卷空间；不拉取镜像",
		Long: "检查工具、本地镜像、18080 端口及至少 1 GiB Docker 卷空间。短暂创建并清理探针。\nchat doctor public / files doctor generated / memory doctor mounts 额外验证 Docker 内的 FUSE、SYS_ADMIN 和挂载权限。根 doctor 只检查公共依赖。",
		Args: func(command *cobra.Command, args []string) error {
			if domain == "" {
				return cobra.NoArgs(command, args)
			}
			if len(args) > 1 {
				return errors.New("doctor accepts at most one scenario")
			}
			if len(args) == 1 {
				if _, ok := scenarios[domain+"."+args[0]]; !ok {
					return errors.New("unknown doctor scenario")
				}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			options.Command = "doctor"
			options.Scenario = domain
			if len(args) == 1 {
				options.Scenario += "." + args[0]
			}
			return validateOptions(*options)
		},
	}
	command.Flags().StringVar(&options.CloudConfig, "cloud-config", "", "专用云测试配置，仅用于 cloud-storage / cloud-renewal")
	return command
}

func validateOptions(options cliOptions) error {
	if options.Timeout < 0 || (options.Timeout > 0 && options.Timeout < time.Second) {
		return errors.New("--timeout must be at least 1s")
	}
	if options.Command == "doctor" && options.Timeout != 0 {
		return errors.New("--timeout requires a scenario")
	}
	if options.Diagnostics && options.Baseline != "" {
		return errors.New("diagnostic runs cannot be compared with a baseline")
	}
	return nil
}

func needsWorker(selected string) bool {
	if isCloudScenario(selected) {
		return false
	}
	return selected == "files.generated" || selected == "memory.mounts" || selected == "chat" || strings.HasPrefix(selected, "chat.")
}
