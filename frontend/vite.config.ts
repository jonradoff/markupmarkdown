import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 4720,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://localhost:4721",
        changeOrigin: true,
      },
    },
  },
});
