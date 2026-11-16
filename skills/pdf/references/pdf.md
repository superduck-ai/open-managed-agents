# PDF 和 Markdown

用 `pypdf` 合并、拆分、旋转、提取文本和处理表单；表格提取用 `pdfplumber`，扫描件先 `pdftoppm` 转图片，再 `tesseract page.png stdout -l chi_sim+eng` 做 OCR。OCR 输出需抽查，不能当成无误的原文。

Markdown 转 PDF：先 `pandoc input.md -o /workspace/task/input.docx`，再用 render.py 转 PDF。中文 DOCX 样式使用 Noto CJK。需要精确字体控制可用 Pandoc reference DOCX。ReportLab 可直接创建 PDF；中文需注册可用字体，例如：

```python
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.cidfonts import UnicodeCIDFont
from reportlab.pdfgen import canvas

pdfmetrics.registerFont(UnicodeCIDFont('STSong-Light'))
page = canvas.Canvas('/mnt/user-data/outputs/摘要.pdf')
page.setFont('STSong-Light', 14)
page.drawString(50, 780, '办公文档摘要')
page.save()
```

CID 字体的阅读器兼容性需要检查；Poppler 提取和渲染 Adobe-GB1 等字符集合需要镜像预装 poppler-data，不能只装 poppler-utils。可移植交付优先嵌入支持的字体。`qpdf --check` 验证结构，`pdfinfo` 核对页数，`pdftotext` 核对预期文本，`pdftoppm` 渲染后查看全部页面。表单填充检查字段是否可见以及是否需要 flatten；保留输入原件。加密、签名或权限限制需按用户授权处理并报告限制。
