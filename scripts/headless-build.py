#!/usr/bin/env python3
"""Prove a headless distribution builds without workspace source or assets."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
environment = {**os.environ, 'GOFLAGS': '-buildvcs=false'}
with tempfile.TemporaryDirectory(prefix='beepgrid-headless-') as directory:
    source = Path(directory) / 'source'
    # Copy only Go build inputs, including engine migration embeds. In particular,
    # there is no package.json, frontend source, generated workspace or Node binary.
    for folder, directories, files in os.walk(root):
        directories[:] = [name for name in directories if name not in {'.git', '.local', 'node_modules', '.venv', 'dist', 'workspace'}]
        for name in files:
            path = Path(folder) / name
            relative = path.relative_to(root)
            if path.suffix not in {'.go', '.sql'} and relative.as_posix() not in {'go.mod', 'go.sum', 'api/openapi.json'}:
                continue
            target = source / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
    result = subprocess.check_output(['go', 'list', '-tags', 'headless', '-json', './internal/web'], cwd=source, env=environment)
    package = json.loads(result)
    assert not package.get('EmbedFiles'), package.get('EmbedFiles')
    assert package['GoFiles'] == ['headless.go'], package['GoFiles']
    subprocess.run(['go', 'build', '-trimpath', '-tags', 'headless', '-o', str(Path(directory)/'notif'), './cmd/notif'], cwd=source, env=environment, check=True)
    print('Headless build passed with no frontend source, assets or JavaScript manifest')
