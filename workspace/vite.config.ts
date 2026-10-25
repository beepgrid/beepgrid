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
    },
  },
  build: { outDir: "dist", sourcemap: true, chunkSizeWarningLimit: 250 },
  test: {
    environment: "jsdom",
    setupFiles: ["./tests/setup.ts"],
    clearMocks: true,
  },
});
