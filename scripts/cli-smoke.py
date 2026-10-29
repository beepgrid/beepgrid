#!/usr/bin/env python3
"""No-browser qualification of actual binaries against disposable PostgreSQL state."""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
database = os.environ['NOTIF_TEST_DATABASE_URL']
schema = 'cli_smoke_' + secrets.token_hex(6)
headless = root / '.local/notif-headless'
workspace = root / '.local/notif'
assert headless.is_file() and workspace.is_file(), 'Build both distributions first'
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
url = f'http://127.0.0.1:{port}'
server = None

with tempfile.TemporaryDirectory(prefix='beepgrid-cli-') as directory:
    directory = Path(directory)
    token = secrets.token_hex(32)
    owner = directory / 'owner-token'
    owner.touch(mode=0o600)
    owner.write_text(token)
    environment = {**{key: value for key, value in os.environ.items() if not key.startswith('NOTIF_')},
        'NOTIF_DATABASE_URL': database, 'NOTIF_SCHEMA': schema,
        'NOTIF_WORKSPACE': 'cli-smoke', 'NOTIF_ENVIRONMENT': 'test',
        'NOTIF_COMPANY': 'CLI test', 'NOTIF_BOOTSTRAP_TOKEN': token,
        'NOTIF_SECRET_KEY': secrets.token_hex(32), 'NOTIF_ADDR': f'127.0.0.1:{port}',
        'NOTIF_PUBLIC_URL': url, 'NOTIF_URL': url, 'NOTIF_TOKEN_FILE': str(owner),
        'NOTIF_ATTACHMENTS_DIR': str(directory / 'attachments'),
        # Never contact external models or providers in this packaging check.
        'NOTIF_OVERSEER_URL': '', 'NOTIF_OVERSEER_TOKEN': '',
    }
    environment.pop('NOTIF_TOKEN', None)

    def cli(args, body=None, credential=owner, expected=0, binary=headless):
        env = {**environment, 'NOTIF_TOKEN_FILE': str(credential)}
        result = subprocess.run([str(binary), *args], input=None if body is None else json.dumps(body),
                                text=True, capture_output=True, env=env, cwd=directory, timeout=40)
        assert result.returncode == expected, (args, result.returncode, result.stderr)
        raw = result.stdout if expected == 0 else result.stderr
        assert token not in raw, 'Owner credential leaked'
        return json.loads(raw) if raw else None

    def stop():
        global server
        if server is not None:
            server.terminate()
            server.wait(timeout=15)
            server = None

    def start(binary, no_ui=False):
        global server
        server = subprocess.Popen([str(binary), 'serve', *(['--no-ui'] if no_ui else [])],
                                  env=environment, cwd=directory, stdout=subprocess.DEVNULL,
                                  stderr=log)
        for _ in range(100):
            if server.poll() is not None:
                raise AssertionError('Server exited; inspect protected smoke log')
            try:
                with urllib.request.urlopen(url+'/readyz', timeout=1) as response:
                    if response.status == 200:
                        return
            except OSError:
                time.sleep(.1)
        raise AssertionError('Server did not become ready')

    def browser_status(expected):
        try:
            with urllib.request.urlopen(url+'/agents', timeout=5) as response:
                assert response.status == expected
                assert b'root' in response.read()
        except urllib.error.HTTPError as error:
            assert error.code == expected

    with (directory/'server.log').open('w') as log:
        try:
            subprocess.run([str(headless), 'migrate'], env=environment, cwd=directory, check=True, capture_output=True)
            start(headless)
            browser_status(404)
            me = cli(['api', 'GET', '/api/v1/me'])
            person = me['person']['id']
            launcher = directory/'launcher-token'
            subprocess.run([str(headless), 'seed', '--prefix', 'research/', '--out', str(launcher)],
                           env=environment, cwd=directory, check=True, capture_output=True)
            worker = directory/'worker-token'
            session = cli(['enroll', '--input', '-', '--out', str(worker)],
                          {'agent':'research/scout', 'name':'Scout'}, launcher)
            assert session['agent']['id'] != person
            assert worker.stat().st_mode & 0o777 == 0o600
            assert 'token' not in session
            denied = cli(['enroll', '--input', '-', '--out', str(directory/'bad-token')],
                         {'agent':'outside/scout', 'name':'Outside'}, launcher, expected=3)
            assert denied['status'] == 403 and not (directory/'bad-token').exists()
            page = cli(['list', '--limit', '1', 'agents'])
            assert len(page['items']) == 1
            # A worker is not an administrator even though enrollment was owner-authorized.
            cli(['api', '--input', '-', 'POST', '/api/v1/roles'],
                {'name':'Escalation', 'rules':[{'action':'*','target_kind':'*','target_id':'*'}]}, worker, expected=3)
            group = cli(['api', '--input', '-', 'POST', '/api/v1/groups'],
                        {'name':'Research', 'members':[person, session['agent']['id']]})
            assert group['name'] == 'Research'
            question = {'target':{'kind':'person','id':person},'title':'Report format?', 'choices':['Brief','Detailed']}
            args = ['ask', '--input', '-', '--key', 'report-42/format/v1']
            first = cli(args, question, worker)
            repeat = cli(args, question, worker)
            assert first['id'] == repeat['id']
            request_path = '/api/v1/requests/'+first['id']
            cli(['wait', '--timeout', '200ms', first['id']], credential=worker, expected=4)
            assert cli(['api', 'GET', request_path])['state'] == 'open'
            # Restart into the normal workspace binary with no state/credential conversion.
            stop()
            start(workspace)
            browser_status(200)
            assert cli(['api', 'GET', request_path])['id'] == first['id']
            # Exercise the actual workspace's same-origin HttpOnly session contract.
            browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            login = urllib.request.Request(url+'/api/v1/auth/login', json.dumps({'token':token}).encode(),
                                           {'Content-Type':'application/json','Origin':url})
            with browser.open(login, timeout=5) as response:
                assert response.status == 200
            with browser.open(url+request_path, timeout=5) as response:
                assert json.load(response)['state'] == 'open'
            result = cli(['respond', '--input', '-', first['id']],
                         {'outcome':'answered','answer':'Brief','version':first['version']})
            assert result['state'] == 'resolved'
            assert cli(['wait', first['id']], credential=worker)['response']['answer'] == 'Brief'
            # CLI messages use the same conversation that the workspace reads.
            sent = cli(['send', '--key', 'hello/v1', 'person', person, 'Ready'], credential=worker)
            with browser.open(url+'/api/v1/conversations/'+sent['conversation_id'], timeout=5) as response:
                assert 'Ready' in json.dumps(json.load(response))
            source_file = directory / 'upload.pdf'
            source_file.write_bytes(b'%PDF-1.4\nCLI fixture')
            uploaded_cli = cli(['file-upload', '--conversation', sent['conversation_id'], '--media', 'application/pdf', str(source_file)], credential=worker)
            assert cli(['files', sent['conversation_id']], credential=worker)[0]['id'] == uploaded_cli['id']
            downloaded_file = directory / 'download.pdf'
            cli(['file-download', '--out', str(downloaded_file), uploaded_cli['id']], credential=worker)
            assert downloaded_file.read_bytes() == source_file.read_bytes()
            assert downloaded_file.stat().st_mode & 0o777 == 0o600
            # File tools use canonical current access and never read arbitrary host paths.
            def mcp_call(name, arguments):
                request = {'jsonrpc':'2.0','id':100,'method':'tools/call','params':{'name':name,'arguments':arguments}}
                result = subprocess.run([str(headless), 'mcp'], input=json.dumps(request)+'\n',
                    env={**environment, 'NOTIF_TOKEN_FILE':str(worker)}, cwd=directory, text=True, capture_output=True, timeout=10)
                assert result.returncode == 0, result.stderr
                reply = json.loads(result.stdout)['result']
                assert not reply.get('isError'), reply
                return json.loads(reply['content'][0]['text'])
            uploaded = mcp_call('upload_attachment', {'conversation_id':sent['conversation_id'], 'file_name':'brief.pdf',
                'media_type':'application/pdf', 'base64':'JVBERi0xLjQKZml4dHVyZQ=='})
            assert uploaded['id'] in [item['id'] for item in mcp_call('list_attachments', {'conversation_id':sent['conversation_id']})]
            assert mcp_call('download_attachment', {'id':uploaded['id']})['base64'] == 'JVBERi0xLjQKZml4dHVyZQ=='
            # The stdio MCP adapter is also part of the headless binary.
            messages = [{'jsonrpc':'2.0','id':1,'method':'initialize'},
                        {'jsonrpc':'2.0','method':'notifications/initialized'},
                        {'jsonrpc':'2.0','id':2,'method':'tools/list'}]
            mcp = subprocess.run([str(headless), 'mcp'], input='\n'.join(map(json.dumps,messages))+'\n',
                                 env={**environment,'NOTIF_TOKEN_FILE':str(worker)}, cwd=directory,
                                 capture_output=True, text=True, timeout=10)
            assert mcp.returncode == 0, mcp.stderr
            replies = [json.loads(line) for line in mcp.stdout.splitlines()]
            assert any(reply.get('id') == 2 and reply.get('result', {}).get('tools') for reply in replies)
            assert worker.read_text().strip() not in mcp.stdout
            stop()
            start(workspace, no_ui=True)
            browser_status(404)
            assert cli(['wait', first['id']], credential=worker)['response']['answer'] == 'Brief'
            print('Both binaries: scoped enrollment, CLI/MCP, denied escalation, idempotency, timeout, restart, workspace cookie session and shared canonical history passed')
        finally:
            stop()
            subprocess.run(['psql', database, '-v', 'ON_ERROR_STOP=1', '-c', 'DROP SCHEMA IF EXISTS '+schema+' CASCADE'],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
