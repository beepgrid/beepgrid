import { defineConfig } from 'vite';
import { gzipSync } from 'node:zlib';
import { readdirSync, statSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve, join } from 'node:path';
import react from '@vitejs/plugin-react';
export default defineConfig({plugins:[react(),{name:'precompress-assets',writeBundle(options) {
 const directory=resolve(options.dir!);
 for (const name of readdirSync(directory,{recursive:true})) {
 const file=join(directory,String(name));
 if (!statSync(file).isFile() || file.endsWith('.gz')) continue;
 const raw=readFileSync(file), encoded=gzipSync(raw,{level:9});
 if (encoded.length<raw.length*0.95) writeFileSync(file+'.gz',encoded);
 }
 }}],build:{outDir:'../internal/web/dist',emptyOutDir:true},server:{port:5175,proxy:{'/api':'http://localhost:4010','/integrations':'http://localhost:4010'}}});
