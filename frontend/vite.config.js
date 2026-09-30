import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// In development, /api is proxied to the Go gateway so the browser sees a
// single origin, exactly like production behind Caddy.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": {
        target: process.env.GATEWAY_URL || "http://localhost:8080",
        changeOrigin: false,
      },
    },
  },
});
