# Git runtime source contract

OMA 校验仓库 URL、checkout、挂载路径和资源冲突，在生成启动参数前重新校验持久化资源。EM 不重复维护这些输入规则，负责实际 Git 获取和文件系统安全操作。

OMA 的公开资源 API 保留现有 checkout 对象；commit 仅接受完整的 40 位（SHA-1）或 64 位（SHA-256）十六进制 SHA，并统一转为小写，不接受短 SHA。向 EM 下发时使用既有 `git_info.ref` 字符串：省略 checkout 时省略 ref，branch 转为 `refs/heads/<name>`，commit 转为 SHA。Runtime source 不再发送顶层 checkout。

OMA Git 资源支持默认分支、指定分支和完整提交 SHA。新建工作树时，默认分支与指定分支使用 `git clone --depth=1 --single-branch --no-tags`，指定分支额外传入 `--branch <branch>`；完整 commit 使用 `git init`、`git fetch --depth=1 --no-tags` 和 detached checkout。OMA 将分支转为 `refs/heads/<name>`，避免与 tag 混淆。OMA 负责将 mount_path 限制在 `/workspace/` 下；EM 负责路径安全，直接在 mount_path 中准备仓库。已有工作树的处理由 EM 的 sources 模式决定，本次 OMA 合同不新增复用策略或跨 Sandbox 恢复能力。

验证：OMA 测试覆盖完整 SHA 输入校验，以及默认分支、具名分支和提交的 wire 转换；EM sources 测试覆盖浅克隆、指定提交、目录冲突、符号链接和失败清理。发布时需要匹配的新 EM artifact；仅修改源码不会更新正在运行的 Sandbox。

Git 失败终止初始化，沿用已有日志与错误事件；不增加资源失败策略或 Claude settings hook。
