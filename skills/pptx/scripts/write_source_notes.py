import argparse
import csv
import io
import json
from pathlib import Path
import re

from pptx import Presentation


def sourced_notes(plan, source_root):
    notes = []
    for entries in plan:
        parts = []
        for entry in entries:
            source = Path(entry['source']).resolve()
            if not source.is_relative_to(source_root.resolve()):
                raise ValueError('Source must be inside the original material directory')
            text = source.read_text(encoding='utf-8-sig')
            if set(entry) == {'source', 'quote'}:
                quote = entry['quote']
                if not isinstance(quote, str) or not quote.strip() or quote not in text:
                    raise ValueError('Quote is absent from the original source')
                parts.append(quote)
            elif set(entry) == {'source', 'rows'} and source.suffix.lower() == '.csv':
                rows = list(csv.DictReader(io.StringIO(text)))
                indices = entry['rows']
                if not indices or any(type(i) is not int or i < 1 or i > len(rows) for i in indices):
                    raise ValueError('CSV row index is outside the original records')
                for index in indices:
                    row = rows[index - 1]
                    if None in row or None in row.values():
                        raise ValueError('Malformed CSV record')
                    parts.append('，'.join(f'{key}：{value}' for key, value in row.items()) + '。')
            else:
                raise ValueError('Each entry needs source and exactly quote or CSV rows')
        notes.append('\n'.join(parts))
    return notes


def write_notes(input_path, plan_path, output_path, source_root, minimum_chinese=0):
    if Path(output_path).exists():
        raise ValueError('Choose a new output file')
    deck = Presentation(input_path)
    plan = json.loads(Path(plan_path).read_text())
    if not isinstance(plan, list) or len(plan) != len(deck.slides) or any(not isinstance(p, list) or not p for p in plan):
        raise ValueError('Provide one nonempty source list for every slide')
    notes = sourced_notes(plan, Path(source_root))
    if any(len(re.findall(r'[\u4e00-\u9fff]', text)) < minimum_chinese for text in notes):
        raise ValueError('Select more relevant source material to meet the requested note length')
    for slide, text in zip(deck.slides, notes):
        slide.notes_slide.notes_text_frame.text = text
    deck.save(output_path)
    saved = Presentation(output_path)
    if [slide.notes_slide.notes_text_frame.text for slide in saved.slides] != notes:
        raise ValueError('Saved notes do not match the validated sources')
    return {'slides': len(notes), 'chinese_characters': [len(re.findall(r'[\u4e00-\u9fff]', text)) for text in notes]}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Write source-verified excerpts and CSV records into PPTX notes')
    parser.add_argument('input')
    parser.add_argument('plan')
    parser.add_argument('output')
    parser.add_argument('--source-root', required=True)
    parser.add_argument('--min-chinese', type=int, default=0)
    args = parser.parse_args()
    print(json.dumps(write_notes(args.input, args.plan, args.output, args.source_root, args.min_chinese), ensure_ascii=False))
