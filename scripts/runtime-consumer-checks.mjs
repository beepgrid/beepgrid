import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
await import('./runtime-ai-sdk-check.mjs');
const root = fileURLToPath(new URL('../', import.meta.url));
execFileSync(process.execPath, [
  root + 'sdk/typescript/node_modules/typescript/bin/tsc',
  '--noEmit', '--strict', '--skipLibCheck', '--module', 'NodeNext', '--target', 'ES2022',
  root + 'tools/runtime-consumers/claude-types.ts',
], { cwd: root, stdio: 'inherit' });
console.log('PASS Claude Agent SDK 0.3.316 published input types, no model network');
