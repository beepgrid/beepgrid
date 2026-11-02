#!/usr/bin/env python3
"""Run the published developer tours against a disposable PostgreSQL-backed engine.

Uses a deterministic OpenAI-compatible model fixture. This verifies the adapter
and Coordinator lifecycle, not live provider quality or third-party delivery.
"""
import contextlib
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request

root = Path(__file__).resolve().parents[1]

def free_port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]

class Model(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        task = json.loads(body['messages'][-1]['content'])
        if task['task'] == 'overseer_plan':
            ids = [s['agent']['id'] for s in task['sources']][:2]
            result = dict(operation='question', agent_ids=ids, body='Is staging ready?', summary='Collect release blockers.', instructions=[])
        else:
            result = {'summary': 'Specialist replies are still pending.'}
        raw = json.dumps({'choices': [{'finish_reason': 'stop', 'message': {'role': 'assistant', 'content': json.dumps(result)}}]}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

schema = 'doc_tour_' + secrets.token_hex(8)
database = os.getenv('NOTIF_TEST_DATABASE_URL', 'postgres://notif:notif@localhost:5499/notif?sslmode=disable')
model = ThreadingHTTPServer(('127.0.0.1', 0), Model)
threading.Thread(target=model.serve_forever, daemon=True).start()
port = free_port()
origin = f'http://localhost:{port}'
with tempfile.TemporaryDirectory(prefix='beepgrid-doc-tour-') as scratch:
    credential = Path(scratch) / 'owner-token'
    credential.touch(mode=0o600)
    credential.write_text(secrets.token_hex(32))
    env = {**{key: value for key, value in os.environ.items() if not key.startswith('NOTIF_')}, 'NOTIF_DATABASE_URL': database, 'NOTIF_SCHEMA': schema,
           'NOTIF_ADDR': f'127.0.0.1:{port}', 'NOTIF_PUBLIC_URL': origin,
           'NOTIF_WORKSPACE': 'documentation-tours', 'NOTIF_SECRET_KEY': secrets.token_hex(32),
           'NOTIF_BOOTSTRAP_TOKEN': credential.read_text(), 'NOTIF_URL': origin, 'NOTIF_TOKEN_FILE': str(credential),
           'NOTIF_ATTACHMENTS_DIR': str(Path(scratch) / 'files'), 'PYTHONPATH': str(root / 'sdk/python'),
           'NOTIF_AI_API_KEY': '', 'NOTIF_OVERSEER_URL': '', 'NOTIF_OVERSEER_TOKEN': '', 'NOTIF_TOKEN': '',
           'NOTIF_DOC_BINARY': str(root / '.local/notif-doc-tour'), 'NOTIF_AI_PROVIDER': 'openai-compatible', 'NOTIF_AI_BASE_URL': f'http://127.0.0.1:{model.server_port}/v1',
           'NOTIF_AI_ALLOW_PRIVATE': 'true', 'NOTIF_AI_MODEL': 'documentation-fixture', 'NOTIF_AI_OUTPUT_MODE': 'json'}
    binary = root / '.local/notif-doc-tour'
    binary.parent.mkdir(exist_ok=True)
    subprocess.run(['go', 'build', '-buildvcs=false', '-o', str(binary), './cmd/notif'], cwd=root, check=True)
    subprocess.run([str(binary), 'migrate'], env=env, cwd=root, check=True)
    with open(Path(scratch) / 'server.log', 'w+') as log:
        server = subprocess.Popen([str(binary), 'serve'], env=env, cwd=root, stdout=log, stderr=log)
        try:
            for _ in range(100):
                try:
                    with urllib.request.urlopen(origin + '/readyz', timeout=1): break
                except OSError:
                    if server.poll() is not None:
                        log.seek(0); raise RuntimeError(log.read())
                    time.sleep(.1)
            else: raise RuntimeError('Engine did not become ready')
            commands = [([sys.executable, 'examples/developer-tour/main.py'], 'python')]
            if (root / 'examples/developer-tour/main.ts').exists():
                subprocess.run(['npm', '--prefix', 'sdk/typescript', 'run', 'build'], cwd=root, check=True)
                subprocess.run(['npm','--prefix','examples/developer-tour','ci','--ignore-scripts','--no-fund','--no-audit'],cwd=root,check=True)
                subprocess.run(['npm','--prefix','examples/developer-tour','run','check'],cwd=root,check=True)
                commands.append((['node', '--experimental-strip-types', 'examples/developer-tour/main.ts'], 'typescript'))
            if (root / 'examples/developer-tour/main.go').exists():
                commands.append((['go', 'run', '-buildvcs=false', './examples/developer-tour/main.go'], 'go'))
            commands.append(([sys.executable, 'examples/developer-tour/advanced.py'], 'advanced'))
            commands.append(([sys.executable, 'examples/developer-tour/tools.py'], 'mcp'))
            commands.append(([sys.executable, 'examples/a2a-workflows/api-migration.py'], 'api-migration'))
            commands.append(([sys.executable, 'examples/a2a-workflows/incident.py'], 'incident-investigation'))
            evidence = []
            for command, language in commands:
                if language in ['python','typescript','go']:
                    subprocess.run(command, env={**env,'NOTIF_TOUR_MESSAGES_ONLY':'1'},cwd=root,check=True,timeout=60)
                time.sleep(1.1)
                subprocess.run(command, env=env, cwd=root, check=True, timeout=150)
                source = root / command[-1].removeprefix('./')
                evidence.append({'language': language, 'file': str(source.relative_to(root)), 'sha256': hashlib.sha256(source.read_bytes()).hexdigest(), 'status': 'passed'})
            out = root / '.local/documentation-qualification.json'
            recipe_names = {'api-migration', 'incident-investigation'}
            out.write_text(json.dumps({'status': 'passed', 'environment': 'local PostgreSQL, real engine HTTP, deterministic model fixture',
                'tours': [item for item in evidence if item['language'] not in recipe_names],
                'recipes': [item for item in evidence if item['language'] in recipe_names],
                'recipe_source_hashes': {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in (root/'examples/a2a-workflows').glob('*.py')},
                'source_hashes': {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for folder in ['sdk/go/notif','sdk/typescript/src','sdk/python/beepgrid','sdk/react/src'] for p in (root/folder).glob('*') if p.suffix in ['.go','.ts','.tsx','.py'] and not p.name.endswith('_test.go')}}, indent=2) + '\n')
            print('PASS: documentation tours; evidence: .local/documentation-qualification.json')
        finally:
            server.terminate()
            try: server.wait(timeout=10)
            except subprocess.TimeoutExpired: server.kill(); server.wait()
            subprocess.run(['psql', database, '-v', 'ON_ERROR_STOP=1', '-c', f'DROP SCHEMA IF EXISTS {schema} CASCADE'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            model.shutdown()
