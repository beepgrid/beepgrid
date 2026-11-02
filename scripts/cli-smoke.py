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
database = os.environ['BEEPGRID_TEST_DATABASE_URL']
schema = 'cli_smoke_' + secrets.token_hex(6)
headless = root / '.local/beepgrid-headless'
workspace = root / '.local/beepgrid'
assert headless.is_file() and workspace.is_file(), 'Build both distributions first'
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
url = f'http://127.0.0.1:{port}'
server = None

with tempfile.TemporaryDirectory(prefix='beepgrid-cli-') as directory:
    directory = Path(directory)
    for binary in (headless, workspace):
        result = subprocess.run([str(binary)], cwd=directory, text=True, capture_output=True,
                                env={key: value for key, value in os.environ.items() if not key.startswith('BEEPGRID_')}, timeout=10)
        assert result.returncode == 0 and 'beepgrid serve --dev' in result.stdout, (result.returncode, result.stderr)
        assert not result.stderr and not (directory / '.local').exists(), 'Bare command started setup or wrote local state'
    token = secrets.token_hex(32)
    owner = directory / 'owner-token'
    owner.touch(mode=0o600)
    owner.write_text(token)
    environment = {**{key: value for key, value in os.environ.items() if not key.startswith('BEEPGRID_')},
        'BEEPGRID_DATABASE_URL': database, 'BEEPGRID_SCHEMA': schema,
        'BEEPGRID_WORKSPACE': 'cli-smoke', 'BEEPGRID_ENVIRONMENT': 'test',
        'BEEPGRID_COMPANY': 'CLI test', 'BEEPGRID_BOOTSTRAP_TOKEN': token,
        'BEEPGRID_SECRET_KEY': secrets.token_hex(32), 'BEEPGRID_ADDR': f'127.0.0.1:{port}',
        'BEEPGRID_PUBLIC_URL': url, 'BEEPGRID_URL': url, 'BEEPGRID_TOKEN_FILE': str(owner),
        'BEEPGRID_ATTACHMENTS_DIR': str(directory / 'attachments'),
        # Never contact external models or providers in this packaging check.
        'BEEPGRID_OVERSEER_URL': '', 'BEEPGRID_OVERSEER_TOKEN': '',
    }
    environment.pop('BEEPGRID_TOKEN', None)

    def startup_checks(binary):
        # All cases use this disposable database and private temporary files.
        # The already-running fixture also proves that an occupied port is left alone.
        with socket.socket() as unavailable:
            unavailable.bind(('127.0.0.1', 0))
            unavailable_url = f'postgres://fixture:private_password@127.0.0.1:{unavailable.getsockname()[1]}/beepgrid?sslmode=disable&connect_timeout=1'
            cases = [
                (['serve'], {'BEEPGRID_BOOTSTRAP_TOKEN': '', 'BEEPGRID_SECRET_KEY': ''}, {1}, 'server credentials'),
                (['serve'], {'BEEPGRID_SECRET_KEY': 'private_invalid_key'}, {1}, 'encryption key'),
                (['serve', '--dev'], {'BEEPGRID_ADDR': '0.0.0.0:4010'}, {2}, 'local listen address'),
                (['serve'], {'BEEPGRID_PUBLIC_URL': 'https://private_user:private_password@example.com/path'}, {1}, 'public server URL'),
                (['serve'], {'BEEPGRID_DATABASE_URL': unavailable_url}, {1, 4}, 'connect to PostgreSQL'),
                (['migrate'], {'BEEPGRID_DATABASE_URL': 'postgres://private_user:private_password@localhost:invalid/db'}, {1}, 'BEEPGRID_DATABASE_URL'),
                (['serve'], {'BEEPGRID_SCHEMA': 'cli_empty_' + secrets.token_hex(6)}, {1}, 'not initialized'),
                (['serve'], {}, {1}, 'already in use'),
                (['serve'], {'BEEPGRID_AI_ALLOW_PRIVATE': 'not-a-boolean'}, {1}, 'AI configuration'),
                (['serve'], {'BEEPGRID_AI_PROVIDER': 'invalid', 'BEEPGRID_AI_MODEL': 'fixture'}, {1}, 'AI provider'),
                (['serve'], {'BEEPGRID_PROFILE_ADDR': '0.0.0.0:6060'}, {1}, 'diagnostics listener'),
                (['serve'], {'BEEPGRID_OIDC_ISSUER': 'http://example.invalid'}, {1}, 'sign-in provider'),
                (['serve'], {'BEEPGRID_OVERSEER_URL': 'not-a-url'}, {1}, 'Coordinator endpoint'),
                (['serve', '--help'], {}, {0}, 'Usage: beepgrid serve'),
                (['mcp', '--help'], {}, {0}, 'Usage: beepgrid mcp'),
                (['migrate', '--help'], {}, {0}, 'Usage: beepgrid migrate'),
                (['trace', '--help'], {}, {0}, 'Usage: beepgrid trace'),
                (['serve', '--invalid-option'], {}, {2}, 'flag provided but not defined'),
                (['migrate', '--invalid-option'], {}, {2}, 'flag provided but not defined'),
                (['--json', 'serve'], {'BEEPGRID_BOOTSTRAP_TOKEN': '', 'BEEPGRID_SECRET_KEY': ''}, {1}, 'server credentials'),
                (['list', 'agents'], {'BEEPGRID_TOKEN_FILE': '', 'BEEPGRID_TOKEN': ''}, {2}, 'BEEPGRID_TOKEN_FILE'),
                (['list', 'agents'], {'BEEPGRID_URL': 'https://private_user:private_password@example.com'}, {2}, 'BEEPGRID_URL'),
            ]
            for args, changes, expected, marker in cases:
                with tempfile.TemporaryDirectory(dir=directory) as cwd:
                    result = subprocess.run([str(binary), *args], cwd=cwd, env={**environment, **changes}, text=True, capture_output=True, timeout=12)
                    text = result.stdout if result.returncode == 0 else result.stderr
                    assert result.returncode in expected and marker in text, (args, result.returncode, text)
                    for private in (token, 'private_user', 'private_password', 'private_invalid_key'):
                        assert private not in text, 'Private configuration appeared in an error'
                    assert 'panic:' not in text and 'Beep Grid ready' not in text
                    if result.returncode == 0:
                        assert not result.stderr and not (Path(cwd) / '.local').exists()
                    elif text.startswith('{'):
                        error = json.loads(text)
                        assert error['exit_code'] == result.returncode and error['hint'] and error['docs_url'].startswith('https://')
                    else:
                        assert len(text.strip().splitlines()) == 3 and 'Guide: https://' in text
                    with urllib.request.urlopen(url+'/readyz', timeout=2) as response:
                        assert response.status == 200, 'Startup failure disturbed the running fixture'

        if os.name != 'nt':
            import pty
            for force_json in (False, True):
                args = (['--json'] if force_json else []) + ['list', 'agents']
                pid, master = pty.fork()
                if pid == 0:
                    os.chdir(directory)
                    os.execve(str(binary), [str(binary), *args], {**environment, 'BEEPGRID_TOKEN_FILE': '', 'BEEPGRID_TOKEN': ''})
                try:
                    chunks = []
                    while True:
                        try:
                            chunk = os.read(master, 4096)
                            if not chunk: break
                            chunks.append(chunk)
                        except OSError:
                            break
                    text = b''.join(chunks).decode()
                    _, status = os.waitpid(pid, 0)
                    assert os.waitstatus_to_exitcode(status) == 2
                    if force_json:
                        assert json.loads(text)['exit_code'] == 2
                    else:
                        assert not text.startswith('{') and len(text.strip().splitlines()) == 3 and 'Guide:' in text
                finally:
                    os.close(master)

    def cli(args, body=None, credential=owner, expected=0, binary=headless):
        env = {**environment, 'BEEPGRID_TOKEN_FILE': str(credential)}
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
            startup_checks(headless)
            startup_checks(workspace)
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
            # The private staging command associates bytes with the initial message.
            staged_cli = cli(['file-stage', '--media', 'application/pdf', str(source_file)], credential=worker)
            assert not staged_cli.get('conversation_id')
            staged_message = cli(['send', '--input', '-', '--key', 'staged-cli/v1'],
                {'target':{'kind':'person','id':person}, 'body':'Initial CLI file', 'attachment_ids':[staged_cli['id']]}, worker)
            assert staged_message['messages'][0]['attachments'][0]['id'] == staged_cli['id']
            # File tools use canonical current access and never read arbitrary host paths.
            def mcp_call(name, arguments):
                request = {'jsonrpc':'2.0','id':100,'method':'tools/call','params':{'name':name,'arguments':arguments}}
                result = subprocess.run([str(headless), 'mcp'], input=json.dumps(request)+'\n',
                    env={**environment, 'BEEPGRID_TOKEN_FILE':str(worker)}, cwd=directory, text=True, capture_output=True, timeout=10)
                assert result.returncode == 0, result.stderr
                reply = json.loads(result.stdout)['result']
                assert not reply.get('isError'), reply
                return json.loads(reply['content'][0]['text'])
            uploaded = mcp_call('upload_attachment', {'conversation_id':sent['conversation_id'], 'file_name':'brief.pdf',
                'media_type':'application/pdf', 'base64':'JVBERi0xLjQKZml4dHVyZQ=='})
            assert uploaded['id'] in [item['id'] for item in mcp_call('list_attachments', {'conversation_id':sent['conversation_id']})]
            assert mcp_call('download_attachment', {'id':uploaded['id']})['base64'] == 'JVBERi0xLjQKZml4dHVyZQ=='
            staged = mcp_call('stage_attachment', {'file_name':'initial.txt','media_type':'text/plain','base64':'eA=='})
            shared = mcp_call('start_chat', {'targets':[{'kind':'person','id':person}], 'body':'MCP initial file',
                'attachment_ids':[staged['id']], 'idempotency_key':'mcp-shared/v1'})
            assert shared['messages'][0]['attachments'][0]['id'] == staged['id']
            change = {'conversation_id':shared['conversation_id'], 'operation':'rename','version':1,
                'title':'MCP renamed chat','idempotency_key':'mcp-rename/v1'}
            renamed = mcp_call('change_chat_membership', change)
            assert renamed['version'] == 2
            assert mcp_call('change_chat_membership', change) == renamed
            assert cli(['api','GET','/api/v1/conversations/'+shared['conversation_id']], credential=worker)['title'] == 'MCP renamed chat'
            # The stdio MCP adapter is also part of the headless binary.
            messages = [{'jsonrpc':'2.0','id':1,'method':'initialize'},
                        {'jsonrpc':'2.0','method':'notifications/initialized'},
                        {'jsonrpc':'2.0','id':2,'method':'tools/list'}]
            mcp = subprocess.run([str(headless), 'mcp'], input='\n'.join(map(json.dumps,messages))+'\n',
                                 env={**environment,'BEEPGRID_TOKEN_FILE':str(worker)}, cwd=directory,
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
