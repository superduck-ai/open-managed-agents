# PowerPoint

用 `python-pptx` 创建和编辑可编辑 PPTX。读取既有文件时检查幻灯片尺寸、形状位置、文本、表格、图片、颜色及 `slide.notes_slide.notes_text_frame.text` 备注；图片用 `shape.image.blob` 提取。以现有模板为基础编辑可减少风格丢失。

```python
from pptx import Presentation
from pptx.util import Inches

deck = Presentation()
deck.slide_width = Inches(13.333)
deck.slide_height = Inches(7.5)
slide = deck.slides.add_slide(deck.slide_layouts[1])
slide.shapes.title.text = '季度进展'
slide.placeholders[1].text = '填写有证据的进展与下一步'
for shape in slide.shapes:
    if shape.has_text_frame:
        for paragraph in shape.text_frame.paragraphs:
            for run in paragraph.runs:
                run.font.name = 'Noto Sans CJK SC'
slide.notes_slide.notes_text_frame.text = '讲解重点与数据来源'
deck.save('/mnt/user-data/outputs/季度进展.pptx')
```

每页一个主要观点，文字、图片和图表保留可编辑性。用 render.py 导出 PDF、pdftoppm 逐页预览，核对页数、中文、字号、元素边界和备注内容。标题、母版和文本框可能重叠，XML 可读不代表视觉正确。LibreOffice 与 PowerPoint 对复杂字体、动画、SmartArt 和母版渲染可能不同，只有实际检查过的部分才能宣称兼容。

此环境创建本地 PPTX/PDF，不接受 `manus-slides://` URI。Manus 平台幻灯片需先在原服务导出真实文件。

## 原生数据图表

用户要求数据对比图时，用 `CategoryChartData` 和 `slide.shapes.add_chart` 生成可编辑图形。以下仅展示 API，类别、数值和单位必须替换为实际来源：

```python
from pptx.chart.data import CategoryChartData
from pptx.enum.chart import XL_CHART_TYPE, XL_LEGEND_POSITION

data = CategoryChartData()
data.categories = ['部门甲', '部门乙']
data.add_series('预算', (1200, 800))
data.add_series('实际', (1100, 850))
chart = slide.shapes.add_chart(
    XL_CHART_TYPE.COLUMN_CLUSTERED, Inches(0.8), Inches(1.5),
    Inches(7.2), Inches(4.8), data
).chart
chart.has_legend = True
chart.legend.position = XL_LEGEND_POSITION.BOTTOM
chart.legend.include_in_layout = False
chart.category_axis.tick_labels.font.name = 'Noto Sans CJK SC'
chart.value_axis.tick_labels.font.name = 'Noto Sans CJK SC'
assert any(shape.has_chart for shape in slide.shapes)
```

图形和可选明细表都应使用同一组来源数据，合计独立校验。交付前重新打开 PPTX，确认要求图表的页面存在 chart 对象及正确数据系列；渲染后确认柱形、类别、单位和图例全部可读。只创建图表脚本或 PNG 但没有嵌入最终页面，仍未完成图表要求。

## 图片比例

以单一尺寸约束插入图片，保留原始比例，例如：

```python
picture = slide.shapes.add_picture(image_path, Inches(8), Inches(3), width=Inches(4.5))
original_width, original_height = picture.image.size
assert abs((picture.width / picture.height) / (original_width / original_height) - 1) < 0.02
```

分别指定 width 和 height 会拉伸图片。图片需适配固定矩形时，保持比例缩放后留白；只有用户要求裁剪时才使用裁剪。检查流程图与截图中的中文字形比例，保留可读字号，不能将宽流程图拉成方形以填满区域。

## 有来源的演讲备注

附件事实汇报使用 `scripts/write_source_notes.py`，通过从原始附件选择内容生成演讲备注。先在工作目录写一个 JSON 数组，数组项按页排列，每页列出相关的原文摘录或 CSV 数据行。source 是原始附件绝对路径，quote 必须逐字存在于该文件；rows 为不含表头的 CSV 记录序号，从 1 开始。例如一页的计划是：

```json
[
  [
    {"source": "/mnt/session/uploads/材料.md", "quote": "原始材料中的完整句子。"},
    {"source": "/mnt/session/uploads/预算.csv", "rows": [1, 2]}
  ]
]
```

源文件使用用户附件原件；在已有输入文件中选择，不创建虚构来源文件。脚本逐字核验 quote，CSV 按原始列名展开所选记录，然后将这些内容写入每页 notesSlides。每页摘录保持与该页主题相关，字数不足时选更多相关原文或数据行。

```sh
python3 <本Skill目录>/scripts/write_source_notes.py 草稿.pptx 备注计划.json 已核验备注.pptx --source-root /mnt/session/uploads --min-chinese 40
```

`--min-chinese` 仅在用户要求中文备注字数时按要求传入。页数、来源、记录索引或字数不符会退出失败且不发布新 PPTX；修正计划后重跑。导出与交付都使用生成的已核验备注版本，之后更改备注需要重新运行相同来源校验。此脚本只保护从附件摘录生成的备注，不校验幻灯片正文，也不代替逐页视觉检查。
