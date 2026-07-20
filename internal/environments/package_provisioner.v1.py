#!/usr/bin/env python3
"""Install an OMA v1 package manifest without interpreting package specs as shell."""

import json
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Manager:
    name: str
    prepare: tuple[tuple[str, ...], ...]
    install: tuple[str, ...]
    one_spec_per_command: bool = False


MANAGERS = (
    Manager("apt", (("apt-get", "update"),), ("apt-get", "install", "-y", "--")),
    Manager("cargo", (), ("cargo", "install")),
    Manager("gem", (), ("gem", "install")),
    Manager("go", (), ("go", "install"), one_spec_per_command=True),
    Manager("npm", (), ("npm", "install", "--global", "--")),
    Manager("pip", (), ("pip", "install")),
)


def load_manifest(manifest_path: str) -> dict[str, list[str]]:
    manifest = json.loads(Path(manifest_path).read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise ValueError("packages manifest must be an object")
    if manifest.get("version") != 1:
        raise ValueError("unsupported packages manifest version")
    packages = manifest.get("packages")
    if not isinstance(packages, dict):
        raise ValueError("packages manifest must contain an object")
    for manager, specs in packages.items():
        if manager == "type":
            continue
        if manager not in {item.name for item in MANAGERS}:
            raise ValueError(f"unsupported package manager: {manager}")
        if not isinstance(specs, list) or not all(isinstance(spec, str) for spec in specs):
            raise ValueError(f"packages.{manager} must be an array of strings")
    return packages


def install(packages: dict[str, list[str]]) -> None:
    for manager in MANAGERS:
        specs = packages.get(manager.name, [])
        if not specs:
            continue
        print(f"installing {manager.name} packages ({len(specs)})", flush=True)
        for command in manager.prepare:
            run(manager.name, "prepare", command)
        if manager.one_spec_per_command:
            for spec in specs:
                run(manager.name, "install", (*manager.install, spec))
            continue
        run(manager.name, "install", (*manager.install, *specs))


def run(manager: str, stage: str, arguments: tuple[str, ...]) -> None:
    try:
        subprocess.run(
            arguments,
            check=True,
            shell=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except subprocess.CalledProcessError as error:
        raise RuntimeError(
            f"manager={manager} stage={stage} exit_code={error.returncode}"
        ) from None


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: package_provisioner.v1.py MANIFEST", file=sys.stderr)
        return 2
    try:
        install(load_manifest(sys.argv[1]))
    except (OSError, ValueError, json.JSONDecodeError, RuntimeError) as error:
        print(f"package provisioning failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
