// Import this helper into your existing host. No new model or tools are chosen.
import {readFileSync} from 'node:fs';
import {Client} from '../../sdk/typescript/dist/index.js';
import {LiveConnection} from '../../sdk/typescript/dist/runtime/index.js';
import {SQLiteInbox} from '../../sdk/typescript/dist/runtime/node.js';

export function connect(binding, {onError=console.error}={}) {
  const client=new Client(process.env.NOTIF_URL,readFileSync(process.env.NOTIF_TOKEN_FILE,'utf8').trim());
  const store=new SQLiteInbox(process.env.BEEPGRID_INBOX_PATH,process.env.BEEPGRID_CONNECTION_ID);
  const connection=new LiveConnection(client,store,binding,process.env.BEEPGRID_BUSY_MODE||'steer',onError).start();
  return {client,connection,async close(){connection.stop();await connection.done();store.close();}};
}
