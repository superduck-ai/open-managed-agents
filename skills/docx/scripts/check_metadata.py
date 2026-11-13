import argparse
from datetime import date
import json
from pathlib import Path
import re

from docx import Document


LABEL = re.compile(r'^(项目负责人|负责人|编制单位|编制|编写|作者|审核人|审核|批准人|批准|单位|公司|组织|报告周期|统计周期|周报周期)\s*[：:]\s*(.*)$')
ALIASES = {'项目负责人': '负责人', '审核人': '审核', '批准人': '批准', '统计周期': '报告周期', '周报周期': '报告周期'}


def normalized(value):
    return re.sub(r'\s+', '', value).strip('。.;；')


def field(text):
    match = LABEL.match(text.strip().replace('**', ''))
    if match:
        label, value = match.groups()
        return ALIASES.get(label, label), normalized(value)
    week = re.search(r'\d{4}年(?:第)?\d{1,2}周|\d{4}-W\d{1,2}', text)
    if week:
        return '报告周期', week.group()


def dates(value):
    result, year = [], None
    pattern = r'(?:(\d{4})年)?(\d{1,2})月(\d{1,2})日|(?:(\d{4})[-/])?(\d{1,2})[-/](\d{1,2})'
    for match in re.finditer(pattern, value):
        groups = match.groups()
        y, month, day = groups[:3] if groups[1] else groups[3:]
        year = int(y) if y else year
        try:
            date(year or 2000, int(month), int(day))
        except ValueError:
            return ()
        result.append((year, int(month), int(day)))
    return tuple(result)


def inspect(document_path, source_paths):
    sources = {}
    for path in source_paths:
        for line in Path(path).read_text(encoding='utf-8-sig').splitlines():
            item = field(line)
            if item:
                sources.setdefault(item[0], set()).add(item[1])
    document = Document(document_path)
    text = [paragraph.text for paragraph in document.paragraphs]
    for section in document.sections:
        text.extend(paragraph.text for side in [section.header, section.footer] for paragraph in side.paragraphs)
    for table in document.tables:
        for row in table.rows:
            text.extend(cell.text for cell in row.cells)
            if len(row.cells) == 2:
                text.append(row.cells[0].text + '：' + row.cells[1].text)
    unsupported, checked = [], 0
    for line in (line for block in text for line in block.splitlines()):
        item = field(line)
        if not item:
            continue
        label, value = item
        if not value or any(marker in value for marker in ['待确认', '未提供', '待补充', '未指定']) or value in {'-', '—'} or re.fullmatch(r'_+', value):
            continue
        checked += 1
        candidates = sources.get(label, set())
        valid = value in candidates
        if label == '报告周期' and dates(value):
            valid = valid or any(dates(value) == dates(candidate) for candidate in candidates)
        if not valid:
            unsupported.append({'field': label, 'value': value, 'reason': 'No matching field in supplied sources; omit or mark pending confirmation'})
    return {'checked_fields': checked, 'unsupported': unsupported, 'scope': 'Common identity and reporting-period fields only; body facts and generated clock dates require separate review'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Read-only source check for DOCX identity and reporting-period fields')
    parser.add_argument('document')
    parser.add_argument('sources', nargs='+')
    args = parser.parse_args()
    result = inspect(args.document, args.sources)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    raise SystemExit(1 if result['unsupported'] else 0)
