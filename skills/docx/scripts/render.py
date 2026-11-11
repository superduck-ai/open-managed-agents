import argparse
import os
from pathlib import Path
import signal
import subprocess
import tempfile

from check_metadata import inspect


def convert(source, destination, output_format, sources=None):
    source = Path(source).resolve(strict=True)
    destination = Path(destination).resolve()
    if destination == source.parent:
        raise ValueError('Use a separate output directory to preserve the input')
    if not source.is_file():
        raise ValueError('Input must be a file')
    if source.suffix.lower() == '.docx':
        if sources is None:
            uploads = Path('/mnt/session/uploads')
            sources = []
            if uploads.is_dir():
                sources = sorted(path for path in uploads.rglob('*')
                                 if path.is_file() and path.suffix.lower() in {'.md', '.txt', '.csv', '.tsv'})
        if sources:
            result = inspect(source, sources)
            if result['unsupported']:
                fields = ', '.join(sorted({item['field'] for item in result['unsupported']}))
                raise ValueError('DOCX metadata lacks source evidence: ' + fields + '. Remove unsupported values or mark them 待确认, then export again.')
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / (source.stem + '.' + output_format)
    if target.exists():
        raise FileExistsError(target)
    with tempfile.TemporaryDirectory(prefix='oma-office-') as scratch:
        profile = Path(scratch) / 'profile'
        staging = Path(scratch) / 'converted'
        staging.mkdir()
        process = subprocess.Popen(
            ['soffice', '-env:UserInstallation=' + profile.as_uri(), '--headless',
             '--convert-to', output_format, '--outdir', str(staging), str(source)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True,
        )
        previous_termination = signal.getsignal(signal.SIGTERM)

        def terminate(signum, frame):
            raise SystemExit(128 + signum)

        signal.signal(signal.SIGTERM, terminate)
        try:
            stdout, stderr = process.communicate(timeout=120)
        finally:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()
            signal.signal(signal.SIGTERM, previous_termination)
        if process.returncode:
            raise RuntimeError('LibreOffice failed: ' + (stdout + stderr)[-2000:])
        produced = staging / target.name
        if not produced.is_file() or produced.stat().st_size == 0:
            raise RuntimeError('LibreOffice produced no output: ' + (stdout + stderr)[-2000:])
        with produced.open('rb') as incoming, target.open('xb') as outgoing:
            try:
                while chunk := incoming.read(1024 * 1024):
                    outgoing.write(chunk)
            except BaseException:
                target.unlink(missing_ok=True)
                raise
    return target


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('input', type=Path)
    parser.add_argument('output_directory', type=Path)
    parser.add_argument('--format', choices=['pdf', 'xlsx', 'docx', 'pptx'], default='pdf')
    parser.add_argument('--sources', nargs='+', type=Path, help='Original text sources for DOCX metadata; defaults to Session text attachments')
    args = parser.parse_args()
    print(convert(args.input, args.output_directory, args.format, args.sources))
