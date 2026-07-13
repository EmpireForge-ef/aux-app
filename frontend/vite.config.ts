import { defineConfig } from "vite";

export default defineConfig({
  server: {
    proxy: {
      // During development the Go backend runs separately on :8080.
      "/api": "http://localhost:8080",
    },
  },
});
