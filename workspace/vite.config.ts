import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 4040,
    strictPort: true,
    proxy: {
      "/api": "http://127.0.0.1:4010",
      "/auth": "http://127.0.0.1:4010",
      "/integrations/slack/": "http://127.0.0.1:4010",
      "/integrations/telegram/": "http://127.0.0.1:4010",
      "/integrations/discord/": "http://127.0.0.1:4010",
      "/integrations/webhook/": "http://127.0.0.1:4010",
    },
  },
  build: {
    outDir: "dist",
    sourcemap: "hidden",
    manifest: true,
    chunkSizeWarningLimit: 320,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./tests/setup.ts"],
    clearMocks: true,
  },
});
