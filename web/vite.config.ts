import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react-swc";
import { tailwind } from "./tailwind";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  plugins: [react(), tailwind()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    outDir: "../internal/httpapi/webdist",
    emptyOutDir: true,
    assetsInlineLimit: 0,
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: Object.fromEntries(
      ["/api", "/locales"].map((path) => [
        path,
        {
          target: process.env.MAILWAKE_DEV_TARGET ?? "http://127.0.0.1:8080",
          changeOrigin: false,
        },
      ]),
    ),
  },
  test: { environment: "happy-dom", clearMocks: true },
});
