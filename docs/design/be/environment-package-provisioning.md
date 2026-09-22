# Environment Package 安装

Cloud Environment 可以通过与 Claude 兼容的 `config.packages` 配置 `apt`、`cargo`、`gem`、`go`、`npm` 和 `pip` package。HTTP 边界负责校验 package 对象与 spec，将省略的列表规范化为 `[]`，并将编码后的 v1 manifest 限制在 1 MiB 以内。package 配置为空时，保持既有 Sandbox 启动与 runtime 发布流程不变。

对于非空 package 配置，如果环境尚未绑定已完成的预构建模板，Runner 创建 Sandbox、持久化 provider ID，然后执行：

```text
/usr/local/bin/environment-manager provision-packages --protocol v1 --stdin
```

JSON manifest 只通过 stdin 发送。命令结果必须是单个严格的 v1 JSON 值，且不得超过 16 KiB。仅当 status 与进程退出码一致时才继续启动；错误只暴露白名单内的 category、manager 和 stage，不包含 package spec 或原始 stdout/stderr。

Package 安装发生在 rclone 和 Environment Manager 启动之前。安装失败时，Runner 停止 Work 并终止已创建的 Sandbox。安装成功后，Runner 检查 Work heartbeat；如果并发 stop 已经生效，则执行清理，不再启动 rclone 或 Manager。没有 package 的 Environment 保持原有顺序：创建 Code Session、启动 Manager，然后发布 runtime metadata。

安装命令使用独立的 `environment_runner.package_provision_timeout`，默认 2 分钟。E2B 命令超时与本地 context deadline 使用同一预算：前者约束 Sandbox 进程，后者保证网络或 Wait 调用不会无限阻塞。

Sandbox 镜像或自定义 E2B template 必须提供 `/usr/local/bin/environment-manager`，并实现 `provision-packages` v1 合同。持久化清理协调以及事务化 Session event 交接不在本功能范围内。

## 软件包预构建

`Prebuild` 为单个环境提前安装软件包并生成可启动模板。开启 `environment_prebuilds` 后，创建环境或修改未归档环境的软件包会在保存环境的同一事务内插入 River 任务；每个环境独立构建。执行链路为 Aliyun Flow 构建镜像 → CubeSandbox 制作模板 → 绑定环境的 `resolved_template`。

Environment 的 `build_job_id` 直接引用 River 的 bigint 主键 `river_job.id`，不新增构建表。任务类型与队列均为 `environment_prebuild`。River args 保存 workspace、environment、服务商配置标识和 Dockerfile；output 保存远端任务引用及检查点。Dockerfile 在入队时固定基础镜像和软件包输入。

环境行锁协调修改、重试、取消及检查点，所有远端请求在事务之外执行。查询校验任务类型、workspace 和 environment；检查点及绑定 SQL 校验当前 job ID，防止旧任务覆盖新配置或已删除的环境。取消请求在检查点写入时合并，避免并发 worker 丢失取消意图。

软件包更新或清空后，旧任务下次执行时取消；已提交的 Flow 构建会先请求远端取消，失败则保留构建引用并每 5 秒重试，接受取消后结束旧 River job。提交过程中发生更新也会先保存返回的构建引用，再执行取消。已经结束的构建不重复取消；当前 CubeSandbox 模板构建不支持取消，旧结果不会绑定到环境。

提交远端任务前先持久化提交标记。响应丢失时停止自动提交，避免重复创建；查询进度失败时继续轮询。超时、停用或服务商配置变化会停止观察，远端任务可能仍在执行。显式重试创建新 River job 和镜像 tag，使用当前配置，从镜像阶段完整重跑。

模板绑定遵循以下规则：

- 仅修改名称、描述、网络或环境变量时保留原构建；清空软件包时解除绑定并恢复基础模板。
- 已成功的模板继续复用，不因构建开关、基础镜像或服务商设置变化而重建。新建环境、修改软件包及显式重试使用当前配置。
- Runner 通过 `build_job_id != nil && resolved_template != ""` 选择已完成模板并跳过启动时安装；待构建或失败时使用当前基础模板并临时安装。
- 没有关联任务的环境保留原模板与安装链路，手动开始后才建立关联。
- River 清理任务记录不影响成功模板，因此 `build_job_id` 不设外键；详情过期后不再返回 job ID、时间或日志。

### 配置与服务商合同

完整配置见 [`docs/configuration-reference.yaml`](../../configuration-reference.yaml) 的 `environment_prebuilds` 部分。`image.base_image` 支持 tag 或 digest，需包含 registry/repository；构建产物固定写入同一镜像仓库，去掉基础镜像的 tag 和 digest，使用环境 UUID 与 River job ID 组成的新 tag。`image.flow` 保存流水线地址与 token。

Flow 运行请求的 `params` 为 JSON 字符串，其中 `envs` 仅包含 `DOCKERFILE_TEXT`、`IMAGE_REPO` 和 `IMAGE_TAG`。Dockerfile 内容使用 `base64:` 前缀加 Base64 编码传递。

配置按默认值、YAML 覆盖、输入整理、校验的顺序加载。默认值统一由 `defaultConfig()` 提供：`timeout` 为 `1h`，`template.disk_size` 为 `20G`；启用预构建时，超时必须为正数、磁盘大小不能为空，显式无效值不回退为默认值。

CPU 单位为毫核，内存单位为 MiB；CPU/内存为 `0` 时由集群选择默认规格。网络 DNS 填写 IP，出站列表填写 CIDR；配置层只整理首尾空白，具体格式交由模板 API 处理，空列表不发送。`allow_internet` 和 `inject_egress_ca` 默认 `true`，显式 `false` 原样发送。

模板请求遵循 [CubeSandbox v0.7.0 API](https://github.com/TencentCloud/CubeSandbox/blob/v0.7.0/CubeAPI/src/models/mod.rs)，网络字段为 `dns`、`allowOut`、`denyOut`、`allowInternetAccess`、`with_cube_ca`。端点、流水线、输出仓库、模板资源及网络设置计入服务商配置标识；凭据、运行时域名及已快照的基础镜像不参与标识。

私有仓库拉取凭据位于 `template.registry_auth.username` / `password`，必须同时设置或同时留空，仅通过 HTTPS 模板请求发给 CubeSandbox，不进入 River job、配置标识或日志。Flow 推送凭据由流水线私密变量管理。镜像引用使用 `registry/repository:tag`，不带协议。

### API 与控制台

已保存环境的详情页在配置表单下方展示工作队列，通过 `GET /v1/environments/{id}/work` 加载工作项 ID、状态、创建时间和更新时间，并提供加载提示、空状态及错误提示；归档环境仍可查看。新建环境页面仅显示配置表单。

API 沿用 `/v1/environments` 的鉴权和 `beta=true` 要求，返回 `build` 对象。`job_id` 全程使用十进制字符串，避免 JavaScript 大整数精度丢失。

| 路由（相对于 `/v1/environments`） | 行为 |
| --- | --- |
| `GET /{id}/prebuild` | 当前状态、时间、`can_start`、`can_cancel` 和日志能力；有软件包但未关联任务时为 `idle`。 |
| `POST /{id}/prebuild` | 无请求体；活动任务和成功结果直接复用，失败、取消或无成功模板且详情过期时创建新任务，不提供强制重建。 |
| `GET /{id}/prebuild/logs?stage=image\|template&job_id=...&cursor=...` | 读取当前任务指定阶段的日志；过期或不匹配的任务返回冲突。 |
| `POST /{id}/prebuild/cancel` | 请求体为字符串 `job_id`，仅取消当前任务；服务商不支持时明确返回不支持。 |

控制台在软件包标题旁显示预安装状态，详情弹窗呈现两个阶段、状态、发起时间、耗时及可用操作。耗时包含排队与观察时间。操作遵从后端能力，归档环境不显示操作；软件包有未保存修改时显示「待保存」。软件包输入框每次添加一个包，通过「添加」按钮或 Enter 生成独立条目；空白输入或同时输入多个包时禁用按钮，输入法组词确认不提交；创建或保存环境时统一提交草稿。包名、输入框和日志禁用字体连字，确保版本符号逐字符显示。

状态缓存按组织、workspace、环境及已保存的软件包隔离，仅活动任务轮询。日志缓存另按 job ID 和阶段隔离，拼接分页后去除 ANSI 颜色控制码，以纯文本呈现且不入库。前端连续读取分页，追上输出后每 3 秒查询新增日志，直到服务商确认完整；构建结束不提前停读，失败保留已读内容并从失败游标重试。自动跟随末尾，用户上滚时保留位置。Flow 已完成步骤读完后继续下一步。不支持日志的阶段显示失败原因或「此阶段不提供日志」。

### 验证

```bash
go test ./internal/config ./internal/environments ./internal/db -run 'Test(LoadEnvironmentPrebuild|Prebuild|EnvironmentMapper|BuildPackage|NormalizePackages|ValidatePackage|ProvisionPackages)' -count=1
go test ./internal/runtime/e2bruntime -count=1
```

CubeSandbox HTTP 测试验证模板请求与状态映射。真实部署还需验证镜像推送、拉取、模板转换及 Session 回连；镜像与模板构建本身无需 Sandbox 回连 OMA，启动 Session 后的注册、心跳、消息与模型代理需要此连通性。
