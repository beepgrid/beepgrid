#!/usr/bin/env python3
"""Reject retired placeholder branding in current source and generated artifacts."""
from pathlib import Path
import re
import subprocess
import sys
import gzip
import tarfile
import zipfile

root = Path(__file__).resolve().parents[1]
# Historical migration bytes are immutable; the upgrade must name stored fields.
exceptions = {
    'internal/engine/migrations/018.sql',
    'internal/engine/migrations/021.sql',
    'internal/engine/migrations/038.sql',
    'internal/engine/branding_migration_test.go',
    'scripts/branding-check.py',
}
# Keep ordinary protocol notifications and standard-library APIs distinct from branding.
retired = re.compile(r'\b[Nn][Oo][Tt][Ii][Ff]\b|[Nn][Oo][Tt][Ii][Ff]_|_notif\b|_notif_|notif-dev|\b[Nn][Oo][Tt][Ii][Ff][Ff]\b|X-Notif-|Notif(?=[A-Z])')
paths = [root / p for p in subprocess.check_output(['git','ls-files','-z'],cwd=root).decode().split('\0') if p]
for value in sys.argv[1:]:
    p=Path(value).resolve()
    paths.extend(p.rglob('*') if p.is_dir() else [p])
errors=[]
def inspect(name, data):
    if retired.search(name):
        errors.append(name)
        return
    try: raw=data.decode('utf-8')
    except UnicodeError: return
    if retired.search(raw): errors.append(name)

for p in sorted(set(paths)):
    if not p.is_file():continue
    try:name=p.relative_to(root).as_posix()
    except ValueError:name=str(p)
    if name in exceptions:continue
    if p.name.endswith(('.tgz','.tar.gz')):
        with tarfile.open(p) as archive:
            for member in archive.getmembers():
                if not member.isfile(): continue
                stream=archive.extractfile(member)
                if stream is not None: inspect(name+'!'+member.name, stream.read())
    elif p.suffix in {'.whl','.zip'}:
        with zipfile.ZipFile(p) as archive:
            for member in archive.namelist():
                if not member.endswith('/'): inspect(name+'!'+member,archive.read(member))
    elif p.suffix=='.gz':
        inspect(name,gzip.decompress(p.read_bytes()))
    else:
        inspect(name,p.read_bytes())
if errors:
    raise SystemExit('Retired branding remains in:\n'+'\n'.join(errors))
print('PASS: current source, filenames and supplied generated artifacts use Beepgrid branding.')
