import argparse
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

from openpyxl import load_workbook

from render import convert


def export_sheet(source, destination, sheet_name, print_area):
    source = Path(source).resolve(strict=True)
    destination = Path(destination).resolve()
    if destination.exists():
        raise FileExistsError(destination)
    book = load_workbook(source)
    if sheet_name not in book.sheetnames:
        raise ValueError('Unknown worksheet: ' + sheet_name)
    selected = book[sheet_name]
    for sheet in book:
        sheet.sheet_state = 'visible' if sheet is selected else 'hidden'
    book.active = book.index(selected)
    selected.sheet_view.tabSelected = True
    selected.print_area = print_area
    selected.sheet_properties.pageSetUpPr.fitToPage = True
    selected.page_setup.orientation = 'landscape'
    selected.page_setup.paperSize = selected.PAPERSIZE_A4
    selected.page_setup.fitToWidth = 1
    selected.page_setup.fitToHeight = 1
    with tempfile.TemporaryDirectory(prefix='xlsx-preview-') as temporary:
        work = Path(temporary)
        preview = work / 'source' / (source.stem + '.xlsx')
        preview.parent.mkdir()
        book.save(preview)
        rendered = work / 'rendered'
        pdf = convert(preview, rendered, 'pdf')
        subprocess.run(['pdftoppm', '-r', '120', '-png', str(pdf), str(rendered / source.stem)],
                       check=True, capture_output=True, timeout=120)
        images = sorted(rendered.glob('*.png'))
        if not images:
            raise RuntimeError('PDF rasterization produced no PNG')
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(rendered, destination)
    return {'pdf': str(destination / pdf.name), 'png': [str(destination / p.name) for p in images]}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Render one worksheet and its charts to a one-page PDF and actual PNG')
    parser.add_argument('input', type=Path)
    parser.add_argument('output_directory', type=Path)
    parser.add_argument('--sheet', required=True)
    parser.add_argument('--print-area', required=True, help='Cell range covering the complete table and charts, such as A1:Q25')
    args = parser.parse_args()
    print(json.dumps(export_sheet(args.input, args.output_directory, args.sheet, args.print_area), ensure_ascii=False))
