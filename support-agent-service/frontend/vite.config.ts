import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import type { ProxyOptions } from "vite";

function gatewayProxy(): ProxyOptions {
  return {
    target: "http://127.0.0.1:9638",
    changeOrigin: true,
    configure(proxy) {
      proxy.on("proxyReq", (outgoing, request) => {
        // Only this loopback demo's browser origins use the existing Web
        // development origin. Other origins retain Gateway validation.
        const origin = request.headers.origin;
        if (
          origin === "http://127.0.0.1:5174" ||
          origin === "http://localhost:5174"
        ) {
          outgoing.setHeader("Origin", "http://localhost:5173");
        }
      });
    },
  };
}

export default defineConfig({
  plugins: [vue()],
  server: {
    host: "127.0.0.1",
    port: 5174,
    strictPort: true,
    proxy: {
      "/api/v1/support/chat": {
        target: "http://127.0.0.1:8000",
        changeOrigin: true,
      },
      "/api/v1/auth/login": gatewayProxy(),
      "/api/v1/users/me": gatewayProxy(),
      "/health/ready": { target: "http://127.0.0.1:8000", changeOrigin: true },
    },
  },
});
