#!/usr/bin/env python3
"""Start this repository's local engine and two deterministic workers. Never stops unrelated processes."""
import os
import pathlib
import signal
import subprocess
import time
import urllib.request

root = pathlib.Path(__file__).resolve().parents[1]
subprocess.run(['docker', 'compose', 'up', '-d', '--wait'], cwd=root, check=True)
subprocess.run(['go', 'build', '-buildvcs=false', '-trimpath', '-o', '.local/beepgrid', './cmd/beepgrid'], cwd=root, check=True)
children = []
logs = []
log_dir = root / '.local/playground'
log_dir.mkdir(parents=True, exist_ok=True, mode=0o700)

def start(name, args):
    log = open(log_dir / (name + '.log'), 'ab')
    logs.append(log)
    child = subprocess.Popen([str(root / '.local/beepgrid'), *args], cwd=root,
                             stdout=log, stderr=subprocess.STDOUT)
    children.append(child)
    return child

try:
    # Check the port before launching, so an existing server is never mistaken for this child.
    import socket
    with socket.socket() as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(('127.0.0.1', 4010))
    server = start('server', ['serve', '--dev'])
    for _ in range(100):
        if server.poll() is not None:
            raise RuntimeError('Engine exited. Inspect .local/playground/server.log')
        try:
            with urllib.request.urlopen('http://localhost:4010/readyz', timeout=1) as response:
                if response.status == 200:
                    break
        except OSError:
            time.sleep(.1)
    else:
        raise RuntimeError('Engine did not become ready')
    start('scout', ['demo', '--name', 'Scout', '--ask'])
    start('builder', ['demo', '--name', 'Builder'])
    print('Beep Grid: http://localhost:4010/\nOwner token: .local/owner-token (copy into local login)\n'
          'Scout and Builder are deterministic transport examples. Ctrl+C stops these processes; history is retained.', flush=True)
    while True:
        for child in children:
            if child.poll() is not None:
                raise RuntimeError('A playground process exited. Inspect .local/playground/*.log')
        time.sleep(1)
except KeyboardInterrupt:
    pass
finally:
    for child in reversed(children):
        if child.poll() is None:
            child.terminate()
    for child in children:
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
    for log in logs:
        log.close()
