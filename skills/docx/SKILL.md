---
name: docx
description: 创建和编辑 Word 文档、报告和模板，检查中文排版、表格、身份与日期来源，并交付 DOCX 或用户指定的 PDF。
---

# Word 文档

先读 [Word 操作与事实校验](references/docx.md)。使用 Python/python-docx 编辑真实 DOCX，沿用用户模板的页面、样式和章节结构。现成模板里的示例姓名、单位、日期和数值也需要来源，未知字段留空或标待确认。

1. 读取输入及用户要求，区分写作、模板填写、已有文档修改和格式转换。检查正文之外的表格、页眉页脚；修订、文本框等高级结构按需检查 OOXML。
2. 在 `/mnt/user-data/workspace/<任务>/` 保存工作副本和脚本。附件在 `/mnt/session/uploads`，Skill 所在目录只读。使用可用的中文字体，明确标题层级、页边距、表格宽度、分页与图片说明。
3. 保存后重新打开，核对实际段落、表格、图片与用户提供的事实。报告周期以材料明确提供的范围为准，未提供则省略或标待确认；项目执行周期不自动等于报告周期，也不按当前日期推算第几周。运行 `python3 <本Skill目录>/scripts/check_metadata.py 工作稿.docx 原始资料.md` 检查身份、报告周期及标题中的日历周，退出码非零时先修正文档再继续。正文先建立来源事实清单，再逐项填写；风险等级、进度评价、差异原因等材料未提供的栏目省略或标待确认。正文事实仍需回查来源；如需记录生成日期，标为实际系统生成日期，与报告范围区分。
4. 用 `python3 <本Skill目录>/scripts/render.py 工作稿.docx 工作目录/preview --format pdf` 渲染，再用 `pdftoppm` 生成逐页预览。按参考中的“表格分页”设置行数与分页，再查看每页的中文、页码、表格断行、图片和空白页，修正后重渲染。
5. 仅将确认可交付的 DOCX 或明确要求的 PDF 复制到 `/mnt/user-data/outputs`，重新打开最终文件后返回绝对路径下载链接。检查稿、脚本和预览留在工作目录。

依赖为 Python、python-docx、LibreOffice Writer、Poppler 和中文字体。缺项时报告具体依赖，交给环境管理员配置；安装 Skill 不等于安装这些依赖。复杂修订、宏、签名和精确 Word 保真度需要单独验证。

## 环境检查

首次使用本 Skill 时，可运行 `python3 <本Skill目录>/scripts/check_runtime.py --strict`。它读取随包 runtime.json，检查命令、Python 库及本地资源；缺失时报告具体缺项。本地检查通过不代表 Agent 工具、网络、凭据或远程服务可用，这些按任务实际调用验证。
