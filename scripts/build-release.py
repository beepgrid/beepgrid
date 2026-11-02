#!/usr/bin/env python3
"""Build versioned, checksummed engine archives without frontend tooling."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--version', required=True)
parser.add_argument('--output', type=Path, default=root / '.local/release')
parser.add_argument('--target', action='append', help='OS/architecture; repeat to limit the build')
args = parser.parse_args()
version = args.version.removeprefix('v')
if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[a-z0-9.]+)?', version):
    parser.error('version must be a semantic version')
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=True)
targets = args.target or ['darwin/arm64', 'darwin/amd64', 'linux/arm64', 'linux/amd64', 'windows/amd64']
artifacts = []
for target in targets:
    system, architecture = target.split('/')
    for headless in [False, True]:
        suffix = '-headless' if headless else ''
        stem = f'beepgrid_{version}_{system}_{architecture}{suffix}'
        with tempfile.TemporaryDirectory(prefix='beepgrid-release-') as directory:
            stage = Path(directory)
            binary = stage / ('beepgrid.exe' if system == 'windows' else 'beepgrid')
            command = ['go', 'build', '-buildvcs=false', '-trimpath', '-ldflags', f'-s -w -X github.com/beepgrid/beepgrid/internal/cli.Version={version}']
            if headless:
                command += ['-tags', 'headless']
            subprocess.run([*command, '-o', str(binary), './cmd/beepgrid'], cwd=root,
                           env={**os.environ, 'CGO_ENABLED':'0', 'GOOS':system, 'GOARCH':architecture}, check=True)
            for name in ['LICENSE', 'THIRD_PARTY_NOTICES.txt']:
                (stage / name).write_bytes((root / name).read_bytes())
            (stage / 'README.txt').write_text(f'''Beepgrid {version}{suffix}
https://beepgrid.com
Documentation: https://beepgrid.com/docs/
Source: https://github.com/beepgrid/beepgrid

Move beepgrid to a directory on PATH, then run: beepgrid version
PostgreSQL is required to run the engine. No PostgreSQL is needed for CLI/MCP clients.
For local setup, set NOTIF_DATABASE_URL and run: beepgrid serve --dev
For production credentials and migrations, see:
https://github.com/beepgrid/beepgrid/blob/main/docs/OPERATIONS.md
This is the public Apache-2.0 engine. Managed billing and website analytics are excluded.
''')
            archive = output / (stem + ('.zip' if system == 'windows' else '.tar.gz'))
            if system == 'windows':
                with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as bundle:
                    for file in sorted(stage.iterdir()): bundle.write(file, file.name)
            else:
                with tarfile.open(archive, 'w:gz') as bundle:
                    for file in sorted(stage.iterdir()): bundle.add(file, file.name)
            artifacts.append(archive)
            print(archive.name, flush=True)
(output / 'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n' for path in sorted(artifacts)))
