# 技能来源

- xlsx、pptx、pdf、docx：由项目维护者提供，保留原始文件内容。
- guizang-ppt-skill：https://github.com/op7418/guizang-ppt-skill ，固定提交 `93d21bf4a73a851907a6eb00acbf30618236667a`；保留完整源码、AGPL-3.0 许可证和原作者声明。该目录的许可独立于打包代码。

2026-11-11：按仓库格式规范为四个 runtime.json 与归藏 assets/motion.min.js 补齐末尾换行；未调整脚本逻辑或模板。打包排除 .DS_Store、Git/仓库协作元数据及未被运行内容引用的宣传大图。
2026-11-12：移除归藏 `SKILL.md` 中运行时执行 `git fetch/pull` 检查上游更新的 Step 0，避免在只读嵌入式挂载目录中触发无效检查或绕过平台版本控制；同步移除贡献者指南、赞助商与双语 README/规划文档等非运行时文件（保留 `LICENSE` 满足 AGPL-3.0 许可）。
