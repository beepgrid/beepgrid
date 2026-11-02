#!/usr/bin/env python3
"""Fail when the public client/tool surface drifts from its documentation inventory."""
import ast
import hashlib
import os
import json
from pathlib import Path
import subprocess
root = Path(__file__).resolve().parents[1]
metadata = root / 'docs/developer'
methods = json.loads((metadata / 'methods.json').read_text())
module = ast.parse((root / 'sdk/python/beepgrid/__init__.py').read_text())
client = next(x for x in module.body if isinstance(x, ast.ClassDef) and x.name == 'Client')
python = [{'name': m.name, 'signature': m.name + '(' + ast.unparse(m.args) + ')', 'doc': ast.get_docstring(m) or ''}
          for m in client.body if isinstance(m, ast.FunctionDef) and (not m.name.startswith('_') or m.name == '__init__')]
assert python == json.loads((metadata / 'python.json').read_text()), 'Python parameters changed; refresh reference and examples'
assert {m['python'] for m in methods} == {m['name'] for m in python if m['name'] != '__init__'}
for language, command in [('go', ['go', 'run', '-buildvcs=false', './internal/doctools']),
                          ('typescript', ['node', 'scripts/documentation-inventory.mjs'])]:
    actual = json.loads(subprocess.check_output(command, cwd=root, text=True))
    expected = json.loads((metadata / (language + '.json')).read_text())
    canonical = lambda values: sorted(values, key=lambda row: (row['file'], row['name']))
    assert canonical(actual) == canonical(expected), language + ' exports, fields or signatures changed; refresh reference and usage coverage'
    names = ({x['name'] for x in actual if x['kind'] == 'method'} - {'AttentionFor', 'Error'}) if language == 'go' else {m['name'] for x in actual if x['name'] == 'Client' for m in x['methods']}
    assert names == {m[language] for m in methods}, (language, names ^ {m[language] for m in methods})
spec = json.loads((root / 'api/openapi.json').read_text())
operations = json.loads((metadata / 'operations.json').read_text())
assert all(not note['returns'].startswith('The JSON response defined below') for note in operations.values()), 'Replace generic result placeholders with real return contracts'
def validate_refs(value):
    if isinstance(value, dict):
        if '$ref' in value:
            assert value['$ref'].split('/')[-1] in spec['components']['schemas'], 'Unresolved API schema reference: '+value['$ref']
        for child in value.values(): validate_refs(child)
    elif isinstance(value, list):
        for child in value: validate_refs(child)
validate_refs(spec)
assert set(operations) == {method.upper() + ' ' + path for path, group in spec['paths'].items() for method in group}
for path, group in spec['paths'].items():
    for method, operation in group.items():
        assert operation['description'] != path and operation['summary'], (method, path)
assert all(s.get('properties') for s in spec['components']['schemas'].values()), 'A public model still lacks field documentation'
for file, expected in json.loads((metadata / 'surface-hashes.json').read_text()).items():
    assert hashlib.sha256((root/file).read_bytes()).hexdigest()==expected, 'Operator/provider/tool surface changed; review documented settings: '+file
binary=root/'.local/notif-doc-tour'
if binary.exists():
    environment={**os.environ,'NOTIF_URL':'http://localhost:1','NOTIF_TOKEN':'documentation-schema-only','NOTIF_AGENT_ID':''}
    message=json.dumps({'jsonrpc':'2.0','id':1,'method':'tools/list','params':{}})+'\n'
    process=subprocess.run([str(binary),'mcp'],input=message,text=True,capture_output=True,env=environment,check=True)
    assert json.loads(process.stdout)['result']['tools']==json.loads((metadata/'mcp.json').read_text()), 'MCP schemas changed; refresh tools and examples'
print(f'PASS: public Python/TypeScript/React/Go exports, fields, {len(methods)} method mappings and {len(operations)} HTTP operation inventory')
