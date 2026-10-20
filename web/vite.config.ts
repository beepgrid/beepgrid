import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({plugins:[react()],build:{outDir:'../internal/web/dist',emptyOutDir:true},server:{port:5175,proxy:{'/api':'http://localhost:4010','/integrations':'http://localhost:4010'}}});
