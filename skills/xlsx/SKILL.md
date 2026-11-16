---
name: xlsx
description: 创建、编辑和分析 Excel、CSV、TSV，保留公式与原始数据，制作可编辑图表并验证重算结果。
---

# Excel 与结构化表格

先读 [公式、重算与图表](references/xlsx.md)。使用 openpyxl 操作 XLSX，使用 Python csv 读取 CSV/TSV。读取源数据时明确编码、分隔符、表头、日期、金额单位和标识符列；编号与前导零按文本保留。

1. 在 `/mnt/user-data/workspace/<任务>/` 保存输入副本和工作簿；保留原始数据表，再创建分析或汇总表。附件位于 `/mnt/session/uploads`，Skill 目录只读。
2. 用户需要可更新的计算时用公式引用源单元格。分组金额、差额与总计都保持公式；图表保留为原生图表。表头、筛选、冻结窗格、数字格式和列宽服务于阅读。
3. 用 `python3 <本Skill目录>/scripts/render.py 工作稿.xlsx 工作目录/recalculated --format xlsx` 重算。openpyxl 不计算公式，`data_only=True` 只读缓存。分别检查公式和缓存，与独立计算对照，并在临时副本修改一条源数据确认汇总跟随变化。
4. 用户要求汇总 PDF/PNG 时，运行 `python3 <本Skill目录>/scripts/export_sheet.py 重算文件.xlsx 工作目录/preview --sheet 汇总 --print-area A1:Q25`，范围按真实表格与原生图表位置调整，输出目录必须尚不存在。脚本仅渲染所选表，使用 A4 横向整页缩放，生成同一真实排版的 PDF/PNG，保持输入 XLSX 不变。逐页检查中文字号、最后一行、坐标轴及图例；过密时调整打印布局。详见 [单工作表导出](references/xlsx.md#单工作表导出与真实预览)。
5. 仅把通过检查的重算 XLSX 或明确要求的 CSV/PDF 放到 `/mnt/user-data/outputs`。重算后避免再通过 openpyxl 保存并清空缓存。只链接用户要求的成品。

依赖为 Python、openpyxl、LibreOffice Calc、Poppler 和中文字体。缺依赖时报告具体缺项。宏、外链、复杂图表、透视表和精确 Excel 保真度需要单独核验，编辑副本后交付。

## 环境检查

首次使用本 Skill 时，可运行 `python3 <本Skill目录>/scripts/check_runtime.py --strict`。它读取随包 runtime.json，检查命令、Python 库及本地资源；缺失时报告具体缺项。本地检查通过不代表 Agent 工具、网络、凭据或远程服务可用，这些按任务实际调用验证。
