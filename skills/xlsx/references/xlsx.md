# Excel 和 CSV

用 `openpyxl` 读取/编辑 XLSX。CSV 用 Python `csv`，明确编码、分隔符及表头，保留前导零和精确标识符。输入文本以 `= + - @` 开头时确认它是公式还是字面数据，字面数据显式设置为字符串。

公式写入使用英语函数名与逗号。`openpyxl` 不计算公式；`data_only=True` 只能读取缓存。通过 render.py 转换到独立目录重算，再重新打开重算文件验证公式仍在、缓存非空、结果符合独立计算，检查 `#REF!`、`#DIV/0!`、`#VALUE!`、`#NAME?` 和 `#N/A` 等错误。不要用预先算好的值替换用户需要的公式。

用户要求按部门等维度汇总且使用公式时，每个分组的金额也必须是公式。Python 分组后填入固定金额无法响应原始数据变化。保留原始数据工作表，用 `SUMIF`/`SUMIFS` 引用它，差额再引用汇总单元格。例如原始数据 B 列为部门、C/D 列为预算/实际，记录在第2至101行：

```python
summary['B2'] = '=SUMIF(\'原始数据\'!$B$2:$B$101,A2,\'原始数据\'!$C$2:$C$101)'
summary['C2'] = '=SUMIF(\'原始数据\'!$B$2:$B$101,A2,\'原始数据\'!$D$2:$D$101)'
summary['D2'] = '=C2-B2'
```

按实际源数据末行调整公式范围。重算后使用非 `data_only` 模式检查各部门预算、实际、差额、总计均为公式，再用 `data_only=True` 核对缓存。在临时副本里修改一条源金额、重新计算，确认该部门和总计同步变化；交付原始输入对应版本。

```python
from openpyxl import Workbook
from openpyxl.chart import BarChart, Reference

book = Workbook()
sheet = book.active
sheet.title = '预算'
sheet.append(['项目', '金额'])
sheet.append(['研发', 1200])
sheet.append(['运营', 800])
sheet.append(['合计', '=SUM(B2:B3)'])
sheet['B4'].number_format = '#,##0.00'
sheet.freeze_panes = 'A2'
sheet.auto_filter.ref = 'A1:B3'
chart = BarChart()
chart.add_data(Reference(sheet, min_col=2, min_row=1, max_row=3), titles_from_data=True)
chart.set_categories(Reference(sheet, min_col=1, min_row=2, max_row=3))
sheet.add_chart(chart, 'D2')
book.save('/workspace/task/预算.xlsx')
```

重算：`python3 scripts/render.py /workspace/task/预算.xlsx /workspace/task/recalculated --format xlsx`。后续 PDF 和交付 XLSX 均从这个重算文件生成，避免保存 openpyxl 副本导致缓存清空。输出目录不能是输入目录。

导出前将图表放在预留单元格区域内，打印区域包含完整图表，设置纸张方向、缩放和列宽。例如图表放在 A8、宽18厘米高9厘米，汇总区域可设置：

```python
chart.width, chart.height = 18, 9
summary.add_chart(chart, 'A8')
summary.print_area = 'A1:L28'
summary.sheet_properties.pageSetUpPr.fitToPage = True
summary.page_setup.orientation = 'landscape'
summary.page_setup.paperSize = summary.PAPERSIZE_A4
summary.page_setup.fitToWidth = 1
summary.page_setup.fitToHeight = 1
```

用户只要汇总 PDF 时，在临时渲染副本里将其他表设为 hidden，保留公式引用的数据；交付 XLSX 继续显示全部数据表。逐页检查 PDF 和 PNG，三个以上分组的最后一个节点、图例及纵轴都须完整；发现横向拆页或截断就调整打印范围、纸张方向与图表尺寸后重新导出。已有宏用 `keep_vba=True` 加载，但 LibreOffice 重存可能损失宏、复杂图表或数据透视结构；未经验证不要覆盖原件或宣称完整保真。

## 单工作表导出与真实预览

已随包提供 `scripts/export_sheet.py`。用户要求汇总表 PDF/PNG 时，使用重算 XLSX 调用：

```sh
python3 <本Skill目录>/scripts/export_sheet.py recalculated/预算.xlsx preview --sheet 汇总 --print-area A1:Q25
```

打印范围必须覆盖实际表格、图表、图例和坐标轴，按实际布局替换；输出目录必须尚不存在。脚本在临时副本隐藏其余表，设置 A4 横向、宽高各一页，由 LibreOffice 重算并导出 PDF，再用 pdftoppm 渲染同一 PDF 的 PNG。它不改变原始交付 XLSX，也不引入新依赖。返回 JSON 列出实际 PDF 和 PNG 路径。

逐页读取真实 PNG，确认中文字号可读、图表完整、没有横向拆页；过密时先调整工作表列宽、图表尺寸与布局，再导出到新的目录。表格与图表的预览保留实际渲染结果，不能用重新绘制的表格图代替。`render.py --format` 支持 PDF、XLSX、DOCX、PPTX；PNG 由 pdftoppm 渲染 PDF。
