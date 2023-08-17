import react from "@vitejs/plugin-react";
import { CodeInspectorPlugin } from "code-inspector-plugin";
import { PluginOption, defineConfig } from "vite";

// https://vitejs.dev/config/
export default defineConfig({
  build: {
    target: "esnext",
  },
  plugins: [
    react(),
    CodeInspectorPlugin({
      bundler: "vite",
    }) as PluginOption[],
  ],
  define: {
    "process.env": {
      BACKEND_URL: "http://localhost:3320",
    },
  },
});
