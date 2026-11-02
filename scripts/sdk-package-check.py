#!/usr/bin/env python3
"""Build local artifacts and verify consumers outside the repository. Never publish."""
import argparse
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import zipfile

root = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--output", type=pathlib.Path, default=root / ".local/sdk-release")
args = parser.parse_args()
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=True)


def run(command, cwd=root, **kwargs):
    return subprocess.run(list(map(str, command)), cwd=cwd, check=True, text=True, **kwargs)


artifacts = []
for package in ["typescript", "react"]:
    directory = root / "sdk" / package
    run(["npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"], cwd=directory)
    packed = run(["npm", "pack", "--json", "--pack-destination", output], cwd=directory, capture_output=True)
    details = json.loads(packed.stdout)[0]
    names = {file["path"] for file in details["files"]}
    assert "LICENSE" in names and "README.md" in names and "CHANGELOG.md" in names
    assert "dist/index.js" in names and "dist/index.d.ts" in names
    assert not any("node_modules" in name or name.endswith(".test.tsx") for name in names)
    artifacts.append(output / details["filename"])

python = os.environ.get("NOTIF_PACKAGE_PYTHON", sys.executable)
with tempfile.TemporaryDirectory(prefix="beepgrid-sdk-consumer-") as directory:
    scratch = pathlib.Path(directory)
    node = scratch / "node"
    node.mkdir()
    (node / "package.json").write_text(json.dumps({"name":"beepgrid-clean-consumer","private":True,"type":"module"}))
    run(["npm", "install", "--ignore-scripts", "--no-audit", "--no-fund", *artifacts, "react@19.3.1", "react-dom@19.3.1", "@types/react@19", "@types/react-dom@19"], cwd=node)
    (node / "consumer.mjs").write_text('''import assert from 'node:assert/strict';
import { Client } from '@notif/client';
import { browserClient, NotifRequest, NotifConversation } from '@notif/react';
import { createElement } from 'react';
import { renderToString } from 'react-dom/server';
assert.equal(typeof new Client('http://localhost','fixture').uploadAttachment,'function');
assert.equal(typeof new Client('http://localhost','fixture').stageAttachment,'function');
assert.equal(typeof new Client('http://localhost','fixture').changeChatMembership,'function');
assert.equal(typeof new Client('http://localhost','fixture').replaceRequest,'function');
assert.equal(typeof new Client('http://localhost','fixture').rotateCredential,'function');
assert.equal(typeof new Client('http://localhost','fixture').renewCredential,'function');
assert.equal(typeof browserClient('http://localhost').events,'function');
assert.match(import.meta.resolve('@notif/react/styles.css'),/styles.css$/);
assert.equal(typeof NotifConversation,'function');
assert.ok(renderToString(createElement(NotifRequest,{client:new Client('http://localhost',''),id:'request-fixture'})).length>0);
''')
    run(["node", "consumer.mjs"], cwd=node)
    (node / "consumer.ts").write_text('''import { Client, type Attachment, type EventOptions, type StartChatInput, type ChatMembershipInput, type ChatMembershipResult } from '@notif/client';
import { NotifRequest } from '@notif/react';
const client = new Client('http://localhost','fixture');
const upload: Promise<Attachment> = client.uploadAttachment('c',new Blob(['x']),'brief.txt');
const events: EventOptions = { after: 1 };
const shared: StartChatInput = {targets:[{kind:"agent",id:"a"}],body:"👋 @Reviewer 2",mentions:[{principal_id:"a",start:2,end:13}]};
const draft: Promise<Attachment> = client.stageAttachment(new Blob(['x']),'brief.txt');
const membership: ChatMembershipInput = {operation:'add',version:1,members:['peer'],share_history:false};
const changed: Promise<ChatMembershipResult> = client.changeChatMembership('c',membership,'members-1');
const replaced: Promise<{previous: import('@notif/client').RequestItem; replacement: import('@notif/client').RequestItem}> = client.replaceRequest('r',{version:1,body:'Revised'},'key');
const followup: Promise<import('@notif/client').RequestItem> = client.ask({conversation_id:'c',body:'Which version?'});
const keys: Promise<import('@notif/client').Credential[]> = client.credentials();
const replacement: Promise<import('@notif/client').CredentialReplacement> = client.rotateCredential('key',1,300);
const renewal: Promise<import('@notif/client').Credential> = client.renewCredential();
void [upload, draft, changed, replaced, followup, keys, replacement, renewal, events, shared, NotifRequest];
''')
    run([root/"sdk/typescript/node_modules/.bin/tsc", "--noEmit", "--strict", "--module", "nodenext", "--target", "es2022", "consumer.ts"], cwd=node)
    wheel_stage = scratch / "python-source"
    shutil.copytree(root / "sdk/python", wheel_stage, ignore=shutil.ignore_patterns("__pycache__", "*.egg-info", "build", "dist"))
    wheel_output = scratch / "wheel-output"
    wheel_output.mkdir()
    run([python, "-m", "pip", "wheel", "--no-deps", "--wheel-dir", wheel_output, wheel_stage])
    candidates = list(wheel_output.glob("*.whl"))
    assert len(candidates) == 1
    wheel = output / candidates[0].name
    shutil.copy2(candidates[0], wheel)
    with zipfile.ZipFile(wheel) as archive:
        names = set(archive.namelist())
        assert "notif/events.py" in names and any(name.endswith("/licenses/LICENSE") for name in names)
    artifacts.append(wheel)
    venv = scratch / "venv"
    run([python, "-m", "venv", venv])
    vpython = venv / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    run([vpython, "-m", "pip", "install", "--no-deps", "--no-index", wheel])
    run([vpython, "-c", "from notif import Client; from notif.events import read_frames; assert callable(Client.start_chat); assert callable(Client.events); assert callable(Client.upload_attachment); assert callable(Client.download_attachment); assert callable(Client.stage_attachment); assert callable(Client.change_chat_membership); assert callable(Client.rotate_credential); assert callable(Client.renew_credential)"], cwd=scratch)
    go = scratch / "go"
    go.mkdir()
    (go / "go.mod").write_text('module example.com/beepgrid-consumer\n\ngo 1.26.0\n\nrequire github.com/beepgrid/beepgrid v0.0.0\nreplace github.com/beepgrid/beepgrid => ' + json.dumps(str(root)) + '\n')
    (go / "main.go").write_text('''package main
import "github.com/beepgrid/beepgrid/sdk/go/notif"
func main(){c:=notif.New("http://localhost","fixture");_ = c.UploadAttachment;_ = c.StageAttachment;_ = c.ChangeChatMembership;_ = c.RotateCredential;_ = c.RenewCredential;_ = notif.CredentialEdit{Version:1,ClearPermissions:true};_ = notif.ChatMembershipInput{Operation:"add",Version:1,Members:[]string{"peer"}};_ = c.StreamEvents;_ = c.Directory;_ = c.Cancel;_ = c.StartChat;_ = notif.StartChatInput{AttachmentIDs:[]string{"draft"},Mentions:[]notif.Mention{{PrincipalID:"a",Start:2,End:13}}}}
''')
    run(["go", "run", "-buildvcs=false", "."], cwd=go)

git = os.environ.get("NOTIF_GIT", "git")
commit = run([git, "rev-parse", "HEAD"], capture_output=True).stdout.strip()
dirty = bool(run([git, "status", "--porcelain"], capture_output=True).stdout.strip())
manifest = {"source_commit":commit,"source_dirty":dirty,"published":False,"namespaces":"development placeholders pending public release decision","artifacts":[{"file":path.name,"sha256":hashlib.sha256(path.read_bytes()).hexdigest(),"bytes":path.stat().st_size} for path in artifacts],"verified_consumers":["Node ESM","TypeScript declarations","React SSR and stylesheet export","Python wheel in clean venv","Go external module with local replace"],"limitations":["No registry namespace availability or registry installation claim","No publication, deployment or provider account mutation","Go source uses the canonical repository; public version tags and proxy installation remain unverified"]}
(output / "manifest.json").write_text(json.dumps(manifest,indent=2)+"\n")
print("PASS: packed TypeScript/React, Python wheel and clean Go consumer. Artifacts:", output)
