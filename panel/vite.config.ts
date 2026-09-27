import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Сборка кладётся в Go-пакет panelapi и встраивается в бинарник (embed).
// В разработке `npm run dev` проксирует /api на Go-сервер (FW_API, по умолчанию :8080).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  build: {
    outDir: "../server/internal/panelapi/dist",
    emptyOutDir: true,
    // Панель для своих пользователей, один бандл ~170 KB gzip — достаточно.
    chunkSizeWarningLimit: 800,
  },
  test: { environment: "node" },
  server: {
    proxy: {
      "/api": { target: process.env.FW_API ?? "http://localhost:8080", changeOrigin: true },
    },
  },
});
