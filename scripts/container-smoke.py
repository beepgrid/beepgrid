#!/usr/bin/env python3
"""Exercise a built non-root image with a fresh, disposable PostgreSQL schema."""
import http.cookiejar
import json
import os
import pathlib
import platform
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

root = pathlib.Path(__file__).resolve().parents[1]
database = os.environ.get('NOTIF_TEST_DATABASE_URL', 'postgres://notif:notif@localhost:5499/notif?sslmode=disable')
parsed = urllib.parse.urlsplit(database)
container_database = database
if parsed.hostname in ('localhost', '127.0.0.1', '::1'):
    credentials = parsed.netloc.rsplit('@', 1)[0] + '@' if '@' in parsed.netloc else ''
    container_database = urllib.parse.urlunsplit(parsed._replace(netloc=credentials+'host.docker.internal:'+str(parsed.port or 5432)))
suffix = secrets.token_hex(6)
schema, name = 'container_test_'+suffix, 'notif-smoke-'+suffix
image = os.environ.get('NOTIF_TEST_IMAGE', 'notif:local-review')
token = secrets.token_hex(32)
port = int(os.environ.get('NOTIF_CONTAINER_TEST_PORT', '4013'))
url = 'http://localhost:'+str(port)
network = ['--add-host=host.docker.internal:host-gateway'] if platform.system() == 'Linux' else []

def docker(args):
    return subprocess.run(['docker', *args], cwd=root, check=True, capture_output=True, text=True).stdout.strip()

user = docker(['image', 'inspect', image, '--format', '{{.Config.User}}'])
assert user not in ('', '0', 'root', '0:0'), 'Image must use a non-root runtime identity'
started = False
try:
    with tempfile.TemporaryDirectory(prefix='notif-image-') as directory:
        env_file = pathlib.Path(directory)/'runtime.env'
        env_file.touch(mode=0o600)
        env_file.write_text('\n'.join(k+'='+v for k,v in {
            'NOTIF_DATABASE_URL':container_database,'NOTIF_SCHEMA':schema,
            'NOTIF_WORKSPACE':'container-test','NOTIF_ENVIRONMENT':'development',
            'NOTIF_BOOTSTRAP_TOKEN':token,'NOTIF_SECRET_KEY':secrets.token_hex(32),
            'NOTIF_ADDR':'0.0.0.0:4010','NOTIF_PUBLIC_URL':url,
        }.items())+'\n')
        docker(['run','--rm',*network,'--env-file',str(env_file),image,'migrate'])
        docker(['run','-d','--name',name,*network,'--env-file',str(env_file),'-p','127.0.0.1:'+str(port)+':4010',image,'serve'])
        started = True
        for _ in range(100):
            try:
                with urllib.request.urlopen(url+'/readyz',timeout=1) as r:
                    if r.status == 200: break
            except OSError: time.sleep(.1)
        else: raise RuntimeError('Container did not become ready')
        with urllib.request.urlopen(url,timeout=5) as r:
            assert b'root' in r.read(), 'Embedded application is missing'
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        login = urllib.request.Request(url+'/api/v1/auth/login',json.dumps({'token':token}).encode(),{'Content-Type':'application/json','Origin':url})
        with opener.open(login,timeout=5) as r: assert r.status == 200
        with opener.open(url+'/api/v1/me',timeout=5) as r:
            me = json.load(r)
            assert me['identity_type']=='person' and me['admin']
        print('Non-root image: migrate, fresh serve, embedded UI, cookie login and separate Person identity passed')
finally:
    if started: docker(['rm','-f',name])
    subprocess.run(['psql',database,'-v','ON_ERROR_STOP=1','-c','DROP SCHEMA IF EXISTS '+schema+' CASCADE'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
