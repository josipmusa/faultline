import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Everything the app requests under /api is forwarded by the dev server to
// API_TARGET. That forwarding happens in Node, not in the browser, which is
// what makes it something Faultline can see: point API_TARGET at a Faultline
// route and every call the page makes runs through it.
const target = process.env.API_TARGET ?? "https://httpbin.org";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
