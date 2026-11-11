---
name: pptx
description: 创建或修改可编辑 PowerPoint 演示，组织内容、图表和演讲备注，逐页检查后交付 PPTX；支持已配置的本地 Slides 项目。
---

# PowerPoint 演示

普通创作和已有 PPTX 编辑先读 [PPTX 文件指南](references/pptx.md)，使用 python-pptx。工程脚本、素材和检查预览留在工作目录；交付前核对原生图表、备注 XML 与实际渲染。

1. 先从用户材料提取有来源的结论、数据与图片，组织完整大纲，再开始排版。页数遵守用户要求；未指定时按内容需要安排。每页承载一个主要观点，避免用长明细表塞满一页。
2. 用户提供模板时沿用模板尺寸、字体、配色和版式。否则选择一致的画布、标题层级、留白和中文字体。用户要求预算/实际等数据对比图时，使用原始数据创建可编辑的柱状图、折线图等原生 chart，表格可补充明细；对比表格不能替代所要求的图形。按 [原生数据图表](references/pptx.md#原生数据图表) 创建并在交付前检查对应页面至少存在一个 has_chart 对象，再逐页查看真实渲染。插入图片保持原始宽高比：python-pptx 的 add_picture 只传 width 或 height，或按原始比例计算另一边；发布前对照图片像素尺寸与 shape 宽高比例。缺少生成服务时选用图形与真实素材。
3. 在 `/mnt/user-data/workspace/<任务>/` 创作。附件来自 `/mnt/session/uploads`，Skill 目录只读。用户要求依据附件制作事实汇报并包含备注时，按 [有来源的演讲备注](references/pptx.md#有来源的演讲备注) 选择原文摘录或 CSV 记录，使用 `scripts/write_source_notes.py` 写入；它会核验原文来源和备注字数，完成后再渲染这份已写备注的 PPTX。其他创作型备注按用户要求撰写，事实、推断与建议明确区分。
4. 用 `scripts/render.py` 转 PDF，再用 `pdftoppm` 生成逐页图片，查看全部页面。检查中文、字号、图表、对齐、遮挡和截断；修正后重新渲染。结构校验成功不等于视觉检查通过。
5. 用户说“做个 PPT”时，默认仅交付一个可编辑 PPTX。只将验收后的文件发布到 `/mnt/user-data/outputs`，PDF、PNG、HTML、XML、JSON、源脚本和工程留在工作目录；其他格式按明确要求发布。使用最终文件绝对路径提供下载链接。

普通路径依赖 Python、python-pptx、LibreOffice Impress、Poppler 和中文字体，不需要 Slides MCP。内容或备注改变后重新渲染并检查最终 PPTX。模板、动画、SmartArt 和母版的高级保真度需要实际验证。

## 环境检查

首次使用本 Skill 时，可运行 `python3 <本Skill目录>/scripts/check_runtime.py --strict`。它读取随包 runtime.json，检查命令、Python 库及本地资源；缺失时报告具体缺项。本地检查通过不代表 Agent 工具、网络、凭据或远程服务可用，这些按任务实际调用验证。
