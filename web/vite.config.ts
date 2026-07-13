import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

// Match restart-server.sh so local SSE proxying works even when Vite is started directly.
const backendTarget = process.env.VITE_API_PROXY_TARGET ?? "http://127.0.0.1:38080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    allowedHosts: ["oma.duck.ai"],
    proxy: {
      "/api": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/v1": {
        target: backendTarget,
        changeOrigin: false,
      },
      "/auth": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/oauth": {
        target: backendTarget,
        changeOrigin: true,
      },
      "/web-api": {
        target: backendTarget,
        changeOrigin: true,
      },
    },
  },
});
