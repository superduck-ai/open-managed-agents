package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type cliOptions struct {
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
	root.SetHelpFunc(func(command *cobra.Command, _ []string) {
		if helpResult = command.ValidateArgs(command.Flags().Args()); helpResult != nil {
			return
		}
		printHelp(command)
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
		Use: "verify-chat", SilenceErrors: true, SilenceUsage: true,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("missing command; use verify-chat -h")
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	flags := root.PersistentFlags()
	flags.StringVar(&options.WorkerImage, "worker-image", "", "临时覆盖 Worker 镜像，不自动拉取")
	flags.StringVar(&options.Baseline, "baseline", "", "性能场景：与已通过的 report.json 比较，退化则失败")
	flags.StringVar(&options.BackendRef, "backend-ref", "", "性能场景：构建指定 Git 提交的后端，使用当前验证负载")
	flags.BoolVar(&options.Diagnostics, "diagnostics", false, "性能场景：采集后端 CPU、heap、trace；不参与基线比较")
	flags.DurationVar(&options.Timeout, "timeout", 0, "覆盖场景执行期限，例如 5m；不含构建和清理")
	flags.BoolP("help", "h", false, "显示帮助并成功退出，无需 Docker 或本地配置")
	root.AddCommand(&cobra.Command{
		Use: "doctor [场景]", Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("doctor accepts at most one scenario")
			}
			if len(args) == 1 {
				if _, ok := scenarios[args[0]]; !ok {
					return errors.New("unknown doctor scenario")
				}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			options.Command = "doctor"
			if len(args) == 1 {
				options.Scenario = args[0]
			}
			return validateOptions(*options)
		},
	})
	runCommand := &cobra.Command{
		Use: "run", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("missing scenario; use verify-chat run -h")
		},
	}
	for name, selected := range scenarios {
		runCommand.AddCommand(&cobra.Command{
			Use: name, Short: selected.Description, Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				options.Command = "run"
				options.Scenario = command.Name()
				return validateOptions(*options)
			},
		})
	}
	root.AddCommand(runCommand)
	root.SetHelpCommand(&cobra.Command{
		Use: "help [命令 [场景]]",
		RunE: func(command *cobra.Command, args []string) error {
			target, remaining, err := root.Find(args)
			if err != nil {
				return err
			}
			if target == command || len(remaining) != 0 {
				return errors.New("use help [doctor|run [SCENARIO]]")
			}
			return target.Help()
		},
	})
	return root
}

func validateOptions(options cliOptions) error {
	if options.Timeout < 0 || (options.Timeout > 0 && options.Timeout < time.Second) {
		return errors.New("--timeout must be at least 1s")
	}
	if options.Command == "doctor" && options.Timeout != 0 {
		return errors.New("--timeout requires run")
	}
	if (options.Command != "run" || options.Scenario != "chat.performance") && (options.Baseline != "" || options.BackendRef != "" || options.Diagnostics) {
		return errors.New("--baseline, --backend-ref and --diagnostics require run chat.performance")
	}
	if options.Diagnostics && options.Baseline != "" {
		return errors.New("diagnostic runs cannot be compared with a baseline; rerun without --diagnostics for the gate")
	}
	return nil
}

func printHelp(command *cobra.Command) {
	output := command.OutOrStdout()
	topic := strings.TrimPrefix(strings.TrimPrefix(command.CommandPath(), "verify-chat"), " ")
	fmt.Fprintln(output, "verify-chat：聊天后端自验证工具")
	switch topic {
	case "doctor":
		fmt.Fprintln(output, "\n用法: verify-chat [选项] doctor [场景] [选项]")
		fmt.Fprintln(output, "检查工具、本地镜像、18080 端口及 Docker 数据卷空间；短暂创建并清理探针容器。doctor chat.public 额外检查 FUSE 和挂载权限，不拉取镜像。")
	case "run":
		fmt.Fprintln(output, "\n用法: verify-chat [选项] run <场景> [选项]")
		fmt.Fprintln(output, "启动隔离依赖、编译后端、运行真实 Worker、保存报告并清理资源。")
	case "", "help":
		fmt.Fprintln(output, "\n用法: verify-chat [选项] <命令>\n\n命令:\n  doctor          检查工具、本地镜像和端口\n  run <场景>      在隔离环境执行验证并保存证据\n  help [命令]     查看帮助，也可使用 -h / --help")
	default:
		name := strings.TrimPrefix(topic, "run ")
		fmt.Fprintf(output, "\n用法: verify-chat [选项] run %s [选项]\n%s\n默认场景期限: %s\n", name, scenarios[name].Description, scenarios[name].Timeout)
	}
	if topic == "" || topic == "help" || topic == "run" {
		names := make([]string, 0, len(scenarios))
		for name := range scenarios {
			names = append(names, name)
		}
		slices.Sort(names)
		fmt.Fprintln(output, "\n验证场景:")
		for _, name := range names {
			fmt.Fprintf(output, "  %-16s %s\n", name, scenarios[name].Description)
		}
	}
	fmt.Fprintln(output, "\n选项（可放在命令或场景前后）:")
	fmt.Fprint(output, command.LocalFlags().FlagUsages(), command.InheritedFlags().FlagUsages())
	fmt.Fprintln(output, `
镜像选择优先级:
  --worker-image > OMA_WORKER_CONTROL_IMAGE > .verify-chat.local.json 的 worker_image > 公共默认镜像`)
	fmt.Fprintf(output, "  公共默认镜像: %s\n", defaultWorker)
	fmt.Fprintln(output, `  本地配置文件位于仓库根目录且被 Git 忽略；帮助不显示其值或环境变量值。

示例（仓库根目录）:
  just verify-chat doctor
  just verify-chat run chat.roundtrip
  just verify-chat run chat.tools
  just verify-chat run -h
  just verify-chat run chat.tools --help

报告: tmp/verify-chat/<run-id>/report.json 和 report.md
退出码: 0 = 验证通过 / doctor 成功 / 帮助；1 = 验证失败；2 = 参数错误或先决条件阻塞。
范围: 固定模型、真实 Worker；公开启动使用本地 Docker Provider，不验证云平台或 UI。
性能: 固定串行负载，2 次预热 + 20 次采样；无 --baseline 时只测量，有基线时检查退化。`)
}
