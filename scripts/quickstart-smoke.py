#!/usr/bin/env python3
"""Rerun the documented playground on a generated schema. Requires an unused port 4010."""
import json
import os
import pathlib
import secrets
import signal
import socket
import subprocess
import time
import urllib.request
import sys

root = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0,str(root/'sdk/python'))
from beepgrid import Client

with socket.socket() as probe:
    probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    probe.bind(('127.0.0.1',4010))
schema = 'quickstart_test_'+secrets.token_hex(8)
env = dict(os.environ, BEEPGRID_SCHEMA=schema)
database = env.get('BEEPGRID_TEST_DATABASE_URL','postgres://beepgrid:beepgrid@localhost:5499/beepgrid?sslmode=disable')
env['BEEPGRID_DATABASE_URL']=database
child = None
log_path = root/'.local/quickstart-smoke.log'
log_path.parent.mkdir(exist_ok=True,mode=0o700)

def stop():
    global child
    if child and child.poll() is None:
        child.send_signal(signal.SIGINT)
        child.wait(timeout=30)
    child = None

def start(log):
    global child
    child = subprocess.Popen([sys.executable,'scripts/playground.py'],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
    for _ in range(300):
        if child.poll() is not None: raise RuntimeError('Playground exited. Inspect '+str(log_path))
        try:
            with urllib.request.urlopen('http://localhost:4010/readyz',timeout=1) as r:
                if r.status==200: return
        except OSError: time.sleep(.1)
    raise RuntimeError('Playground did not become ready')

try:
    with log_path.open('ab') as log:
        start(log)
        owner = Client('http://localhost:4010',(root/'.local/owner-token').read_text().strip())
        for _ in range(100):
            agents=owner.call('GET','/api/v1/agents')
            requests=owner.call('GET','/api/v1/requests')
            if len(agents)==2 and requests: break
            time.sleep(.1)
        else: raise RuntimeError('Two workers and sample question did not appear')
        builder=next(a for a in agents if a['name']=='Builder')
        sent=owner.send(target={'kind':'agent','id':builder['id']},body='Clean quickstart test',key='quickstart/message')
        for _ in range(100):
            conversation=owner.call('GET','/api/v1/conversations/'+sent['conversation_id'])
            if any(m['sender_id']==builder['id'] for m in conversation.get('messages',[])): break
            time.sleep(.1)
        else: raise RuntimeError('Builder did not reply')
        question=requests[0]
        owner.respond(question['id'],'answered',version=question['version'],answer='Brief summary')
        stop()
        start(log)
        restored=owner.get_request(question['id'])
        assert restored['state']=='resolved'
        history=owner.call('GET','/api/v1/conversations/'+sent['conversation_id'])
        assert len(history['messages'])>=2
        print('Clean README quickstart: two agents, question, authenticated reply, restart and durable history passed')
finally:
    stop()
    subprocess.run(['psql',database,'-v','ON_ERROR_STOP=1','-c','DROP SCHEMA IF EXISTS '+schema+' CASCADE'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
