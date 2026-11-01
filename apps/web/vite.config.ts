import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: "autoUpdate",
      manifest: {
        name: "Omnoda",
        short_name: "Omnoda",
        description: "Every Device. One Mind.",
        theme_color: "#12141a",
        background_color: "#12141a",
        display: "standalone",
        start_url: "/",
        icons: [
          { src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" }
        ]
      }
    })
  ],
  server: { port: 5173, proxy: { "/api": "http://127.0.0.1:8787" } },
  build: { outDir: "dist" }
});
