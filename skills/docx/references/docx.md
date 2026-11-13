# Word

用 `python-docx` 创建或编辑 DOCX；用 `Document(path)` 读取段落和表格，保留已有样式。普通段落遍历不涵盖页眉、页脚、文本框和修订，分析已有文档时按需要检查对应 OOXML 部件。不要把纯文本提取当作完整版式分析。

报告周期与项目周期分开处理。用户只提供项目起止日期而未提供周报区间时，省略报告周期或写“报告周期：待确认”；系统当前日期只能作为明确标注的生成日期，不能推断为周报截止日。仅改写已提供的进度、风险和计划，建议与事实分开标注。

署名、组织、编制单位和负责人等身份字段只填写有明确来源的值；来源缺失时删除字段或标“待确认”。交付前提取文中的身份与日期字段，与输入逐项回查，不能按常见模板自动补全机构和人员。

使用 render.py 导出 DOCX 时，会自动读取 `/mnt/session/uploads` 中的 MD/TXT/CSV/TSV 附件并检查身份与报告周期；发现无来源字段即停止转换，不生成新的 PDF。手动运行或来源不在附件目录时，用 `--sources SOURCE.md SOURCE.csv` 指定原始文本来源。没有文本来源时，此自动检查不启用；用户消息、非文本附件及正文事实仍需人工回查。此门槛限于 render.py，不覆盖直接调用 LibreOffice 或自行保存 DOCX。

保存后、导出前运行 `python3 scripts/check_metadata.py REPORT.docx SOURCE.md SOURCE.csv`（脚本路径以本 Skill 挂载目录为基准）。退出码为0且 unsupported 为空才能交付；否则按提示删除无来源字段或标“待确认”，修改后重跑。此工具只读检查常见身份与报告周期字段，不自动改写文件。来源文本中的字段使用“字段：值”；报告周期必须明确来自报告/统计周期字段，不能拿项目起止日期代替。生成日期与正文结论仍需另行核验。

中文正文设置东亚字体，避免英文正常而中文方框：

```python
from docx import Document
from docx.oxml.ns import qn
from docx.shared import Pt

doc = Document()
style = doc.styles['Normal']
style.font.name = 'Noto Sans CJK SC'
style.font.size = Pt(11)
style.element.get_or_add_rPr().get_or_add_rFonts().set(qn('w:eastAsia'), 'Noto Sans CJK SC')
doc.add_heading('项目进展', 0)
doc.add_paragraph('填写有来源的结论。')
doc.save('/mnt/user-data/outputs/项目进展.docx')
```

设置页面边距、标题层级和表格列宽。截图配解释时让配对内容尽量同页。交付前用 render.py 转 PDF 并检查每页，重新打开 DOCX 核对段落、表格和图片数量。需保留修订或复杂模板时直接修改对应 OOXML 并检查相关部件，不能声称 python-docx 会保留所有高级功能。

## 表格分页

创建表格时按实际记录数分配行：一行表头、每条记录一行、可选一行合计；逐行核对，删除未使用的空行。预计能放在一页的小表格，让表头、明细及合计整体同页：对表中除最后一行外每个单元格的段落设置 `paragraph_format.keep_with_next = True`，每行设置 `w:cantSplit`。这样整张表在剩余空间不足时移到下一页，合计不会孤立。紧邻表格的标题同样设置 keep_with_next。

较长表格允许跨页，在首行设置 `w:tblHeader` 重复表头，每行保留 cantSplit，并让最后一条明细与合计保持同页；不要把超过一页的整表锁在一起。导出后查看每页，确认无孤立合计、空白记录或缺少表头的续表。
