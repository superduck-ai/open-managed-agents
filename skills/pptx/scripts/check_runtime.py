import argparse
import glob
import importlib.util
import json
from pathlib import Path
import shutil
import sys


def inspect(requirements):
    commands = {name: shutil.which(name) for name in requirements.get('commands', [])}
    modules = {name: importlib.util.find_spec(name) is not None for name in requirements.get('modules', [])}
    files = {pattern: any(Path(path).is_file() for path in glob.glob(pattern, recursive=True)) for pattern in requirements.get('files', [])}
    missing = [name for name, value in {**commands, **modules, **files}.items() if not value]
    return {'local_ready': not missing, 'missing': missing, 'commands': commands, 'modules': modules, 'files': files,
            'optional_commands': {name: shutil.which(name) for name in requirements.get('optional_commands', [])},
            'optional_modules': {name: importlib.util.find_spec(name) is not None for name in requirements.get('optional_modules', [])},
            'agent_tools_required': requirements.get('agent_tools', []),
            'services_to_verify': requirements.get('services', []),
            'scope': requirements['scope']}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Check local Skill requirements without reading credentials or calling remote services')
    parser.add_argument('--strict', action='store_true')
    args = parser.parse_args()
    requirements = json.loads((Path(__file__).resolve().parent.parent / 'runtime.json').read_text())
    result = inspect(requirements)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    sys.exit(1 if args.strict and not result['local_ready'] else 0)
