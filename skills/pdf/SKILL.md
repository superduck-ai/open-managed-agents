---
name: pdf
description: 创建、提取、合并、拆分或填写 PDF，处理扫描件 OCR，检查中文字体、页面与表单显示，并交付 PDF。
---

# PDF

按 [PDF 操作指南](references/pdf.md) 选择工具：pypdf 做页面和表单操作，pdfplumber 提取表格，ReportLab 排版，LibreOffice 转换 Office，扫描页使用 Poppler 与 Tesseract。文字可提取不代表原版式已保留，OCR 结果需要抽查。

1. 先检查是否为文本型或扫描型 PDF，确认页数、加密、表单与签名状态，保留输入原件。附件在 `/mnt/session/uploads`。
2. 在 `/mnt/user-data/workspace/<任务>/` 存放脚本、工作稿、OCR 和预览。中文排版使用可用字体，面向跨设备交付时优先嵌入字体；编辑或合并后重新检查目录、页码、书签与旋转方向。
3. Office 转换使用 `python3 <本Skill目录>/scripts/render.py INPUT 工作目录/preview --format pdf`。Markdown 可先由 Pandoc 生成 DOCX。表单填写后查看真实显示，按用户要求决定是否扁平化，并说明可编辑性变化。
4. 用 `qpdf --check`、`pdfinfo`、`pdftotext` 检查结构、页数和预期文字，使用 `pdftoppm` 渲染并查看所有页面。修复字体、截断、空白页和表单不可见问题后再交付。
5. 仅将最终 PDF 或明确要求的提取结果发布到 `/mnt/user-data/outputs`，返回绝对路径链接。默认不附检查图片、脚本和中间 DOCX。

依赖为 Python 与对应 PDF 库；转换需要 LibreOffice/Pandoc，渲染需要 Poppler，OCR 需要 Tesseract 及语言包。只报告实际验证的能力与限制，缺项交给环境管理员配置。

## 环境检查

首次使用本 Skill 时，可运行 `python3 <本Skill目录>/scripts/check_runtime.py --strict`。它读取随包 runtime.json，检查命令、Python 库及本地资源；缺失时报告具体缺项。本地检查通过不代表 Agent 工具、网络、凭据或远程服务可用，这些按任务实际调用验证。
