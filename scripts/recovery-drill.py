#!/usr/bin/env python3
"""Isolated real PostgreSQL dump/restore drill. Creates and drops only generated test databases."""
import json
import os
import pathlib
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request

root = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(root / 'sdk/python'))
from notif import Client, NotifError

base_database = os.environ.get('NOTIF_TEST_DATABASE_URL', 'postgres://notif:notif@localhost:5499/notif?sslmode=disable')
parsed = urllib.parse.urlsplit(base_database)
source_name = 'notif_backup_test_' + secrets.token_hex(6)
target_name = 'notif_restore_test_' + secrets.token_hex(6)
container = os.environ.get('NOTIF_POSTGRES_CONTAINER')
if not container:
    container = subprocess.check_output(['docker', 'compose', 'ps', '-q', 'postgres'], cwd=root).decode().strip()
if not container:
    raise SystemExit('Set NOTIF_POSTGRES_CONTAINER to the isolated PostgreSQL test container')

def command(args, *, env=None, stdin=None, stdout=subprocess.DEVNULL):
    result = subprocess.run(args, cwd=root, env=env, stdin=stdin, stdout=stdout, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError(args[0] + ' failed: ' + result.stderr.decode(errors='replace'))

def database_url(name):
    return urllib.parse.urlunsplit(parsed._replace(path='/' + name))

binary = root / '.local/notif-recovery-test'
binary.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
command(['go', 'build', '-buildvcs=false', '-o', str(binary), './cmd/notif'])
env = dict(os.environ)
env.update(NOTIF_ADDR='127.0.0.1:4012', NOTIF_PUBLIC_URL='http://localhost:4012',
           NOTIF_WORKSPACE='recovery-test', NOTIF_ENVIRONMENT='development', NOTIF_SCHEMA='notif',
           NOTIF_BOOTSTRAP_TOKEN=secrets.token_hex(32), NOTIF_SECRET_KEY=secrets.token_hex(32))
server = None
created = []
started = time.monotonic()

with tempfile.TemporaryDirectory(prefix='notif-recovery-') as directory:
    temp = pathlib.Path(directory)
    def start(database):
        global server
        env['NOTIF_DATABASE_URL'] = database_url(database)
        server = subprocess.Popen([str(binary), 'serve'], cwd=root, env=env,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('Recovery test engine exited before readiness')
            try:
                with urllib.request.urlopen('http://localhost:4012/readyz', timeout=1) as response:
                    if response.status == 200:
                        return
            except OSError:
                time.sleep(.1)
        raise RuntimeError('Recovery test readiness timeout')
    def stop():
        global server
        if server is not None:
            server.terminate()
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()
            server = None
    try:
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 4012))
        for name in (source_name, target_name):
            command(['psql', base_database, '-v', 'ON_ERROR_STOP=1', '-c', 'CREATE DATABASE ' + name])
            created.append(name)
        env['NOTIF_DATABASE_URL'] = database_url(source_name)
        command([str(binary), 'migrate'], env=env)
        start(source_name)
        admin = Client('http://localhost:4012', env['NOTIF_BOOTSTRAP_TOKEN'])
        me = admin.call('GET', '/api/v1/me')['person']
        worker, session = admin.connect('recovery/worker', 'Recovery worker')
        role = admin.call('POST', '/api/v1/roles', {'name': 'Recovery fixture worker', 'rules': [{'action': '*', 'target_kind': '*', 'target_id': '*'}]})
        admin.call('POST', '/api/v1/role-bindings', {'role_id': role['id'], 'subject_kind': 'principal', 'subject_id': session['agent']['id']})
        action = {'name': 'write-test-file', 'executor_id': session['agent']['id'], 'resource': 'local-fixture',
                  'resource_version': '1', 'operation_key': 'test-effect-1', 'arguments': {}}
        request = worker.ask(target={'kind': 'person', 'id': me['id']}, title='Write fixture?', kind='approval', action=action, key='recovery-approval')
        admin.respond(request['id'], 'approved', version=request['version'])
        dump = temp / 'snapshot.dump'
        with dump.open('wb') as output:
            command(['docker', 'exec', '-i', container, 'pg_dump', '-U', parsed.username or 'notif', '-Fc', source_name], stdout=output)
        # Simulated external effect completes after the backup. Its receipt will be absent from the restore.
        intent = worker.call('POST', '/api/v1/requests/' + request['id'] + '/redeem', action)
        (temp / 'external-effect-count').write_text('1')
        worker.call('POST', '/api/v1/executions/' + intent['id'], {'state': 'succeeded', 'result': {'fixture_written': True}})
        stop()
        restore_started = time.monotonic()
        with dump.open('rb') as source:
            command(['docker', 'exec', '-i', container, 'pg_restore', '-U', parsed.username or 'notif', '-d', target_name, '--no-owner', '--exit-on-error'], stdin=source)
        env['NOTIF_DATABASE_URL'] = database_url(target_name)
        recovery_token = temp / 'new-owner-token'
        env['NOTIF_RECOVERY_TOKEN_FILE'] = str(recovery_token)
        command([str(binary), 'reconcile', '--restored'], env=env)
        env['NOTIF_BOOTSTRAP_TOKEN'] = recovery_token.read_text().strip()
        start(target_name)
        restored = Client('http://localhost:4012', env['NOTIF_BOOTSTRAP_TOKEN'])
        assert restored.get_request(request['id'])['state'] == 'superseded'
        try:
            worker.get_request(request['id'])
            raise AssertionError('Restored worker credential was not revoked')
        except NotifError as error:
            assert error.status == 401
        try:
            restored.send(target={'kind': 'agent', 'id': session['agent']['id']}, body='Must remain held')
            raise AssertionError('Restore hold did not block new work')
        except NotifError as error:
            assert error.code == 'RECOVERY_HOLD'
        command([str(binary), 'reconcile', '--resume'], env=env)
        replacement, _ = restored.connect('recovery/worker', 'Recovery worker')
        try:
            replacement.call('POST', '/api/v1/requests/' + request['id'] + '/redeem', action)
            raise AssertionError('An approval from the restored snapshot could execute again')
        except NotifError as error:
            assert error.status == 409
        assert (temp / 'external-effect-count').read_text() == '1'
        report = {'result': 'passed', 'database': 'PostgreSQL 17', 'restored_approval': 'superseded',
                  'old_credentials': 'revoked', 'uncertain_effect': 'not replayed',
                  'automated_restore_seconds': round(time.monotonic() - restore_started, 3),
                  'total_drill_seconds': round(time.monotonic() - started, 3),
                  'manual_external_reconciliation_time': 'not measured', 'production_RPO_RTO': 'not measured'}
        (root / '.local/recovery-drill.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report, indent=2))
    finally:
        stop()
        for name in reversed(created):
            command(['psql', base_database, '-v', 'ON_ERROR_STOP=1', '-c', 'DROP DATABASE IF EXISTS ' + name + ' WITH (FORCE)'])
