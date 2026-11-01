import { DatabaseSync } from "node:sqlite";
import { chmodSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { randomUUID } from "node:crypto";
import type { InboxStore, LiveInput, StoredInput } from "./index.js";

/** Private local custody, suitable for a persistent VM/container volume.
 * One database per workspace + exact worker instance. Never place it in a repo.
 * For ephemeral/serverless hosts implement InboxStore using the host's durable DB. */
export class SQLiteInbox implements InboxStore {
  private db:DatabaseSync;
  private lease = randomUUID();
  private timer:ReturnType<typeof setInterval>;
  private closed=false;
  constructor(path:string, identity:string, private maxPending=1000) {
    if (!identity || path === ":memory:") throw new Error("Persistent path and exact connection identity required");
    mkdirSync(dirname(path),{recursive:true,mode:0o700});
    this.db=new DatabaseSync(path);chmodSync(path,0o600);
    this.db.exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
      CREATE TABLE IF NOT EXISTS identity (id INTEGER PRIMARY KEY CHECK(id=1), value TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS lease (id INTEGER PRIMARY KEY CHECK(id=1), owner TEXT NOT NULL, expires INTEGER NOT NULL);
      CREATE TABLE IF NOT EXISTS inputs (id TEXT PRIMARY KEY, payload TEXT NOT NULL, state TEXT NOT NULL, created INTEGER NOT NULL);`);
    try {
      this.db.exec("BEGIN IMMEDIATE");
      const previous=this.db.prepare("SELECT value FROM identity WHERE id=1").get() as {value:string}|undefined;
      if (previous && previous.value !== identity) throw new Error("Inbox belongs to another connection");
      this.db.prepare("INSERT OR IGNORE INTO identity VALUES(1,?)").run(identity);
      const lease=this.db.prepare("SELECT owner,expires FROM lease WHERE id=1").get() as {owner:string;expires:number}|undefined;
      if(lease && lease.expires>Date.now()) throw new Error("This inbox already has a receiver");
      this.db.prepare("INSERT OR REPLACE INTO lease VALUES(1,?,?)").run(this.lease,Date.now()+15000);
      this.db.exec("COMMIT");
    } catch(error) {this.db.exec("ROLLBACK");this.db.close();throw error;}
    this.timer=setInterval(()=>{try{this.guard();this.db.prepare("UPDATE lease SET expires=? WHERE owner=?").run(Date.now()+15000,this.lease);}catch{/* Subsequent operations fail closed. */}},5000);
    this.timer.unref();
  }
  private guard() {
    if(this.closed)throw new Error("Inbox closed");
    const row=this.db.prepare("SELECT owner,expires FROM lease WHERE id=1").get() as {owner:string;expires:number}|undefined;
    if(row?.owner!==this.lease || row.expires<=Date.now()) throw new Error("Receiver lease lost; restart before receiving");
  }
  put(input:LiveInput) {
    this.guard();
    if(this.get(input.id))return;
    const row=this.db.prepare("SELECT count(*) AS n FROM inputs WHERE state!='accepted'").get() as {n:number};
    if(row.n>=this.maxPending)throw new Error("Local inbox full; receiving paused until inputs are accepted");
    this.db.prepare("INSERT INTO inputs VALUES(?,?,'pending',?)").run(input.id,JSON.stringify(input),Date.now());
    // Accepted engine deliveries expire in 30 days. Keep the dedup horizon longer.
    this.db.prepare("DELETE FROM inputs WHERE state='accepted' AND created<?").run(Date.now()-31*86400000);
  }
  pending():StoredInput[] {
    this.guard();
    return (this.db.prepare("SELECT payload,state FROM inputs WHERE state!='accepted' ORDER BY created,rowid").all() as {payload:string;state:StoredInput['state']}[]).map(row=>({...JSON.parse(row.payload),state:row.state}));
  }
  get(id:string):StoredInput|undefined {
    this.guard();
    const row=this.db.prepare("SELECT payload,state FROM inputs WHERE id=?").get(id) as {payload:string;state:StoredInput['state']}|undefined;
    return row?{...JSON.parse(row.payload),state:row.state}:undefined;
  }
  setState(id:string,state:StoredInput['state']) {
    this.guard();
    if(!["pending","submitting","accepted"].includes(state))throw new Error("Invalid custody state");
    if(!this.db.prepare("UPDATE inputs SET state=? WHERE id=?").run(state,id).changes)throw new Error("Unknown input");
  }
  close() {
    if(this.closed)return;
    clearInterval(this.timer);
    this.db.prepare("DELETE FROM lease WHERE owner=?").run(this.lease);
    this.db.close();this.closed=true;
  }
}
