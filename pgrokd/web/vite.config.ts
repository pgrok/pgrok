import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { CodeInspectorPlugin } from "code-inspector-plugin";
import { fileURLToPath } from "node:url";
import { PluginOption, defineConfig } from "vite";

// https://vitejs.dev/config/
export default defineConfig({
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    target: "esnext",
  },
  plugins: [
    react(),
    tailwindcss(),
    CodeInspectorPlugin({
      bundler: "vite",
    }) as PluginOption[],
  ],
});
