import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactPlugin from "eslint-plugin-react";
import importPlugin from "eslint-plugin-import";
import unicorn from "eslint-plugin-unicorn";

export default [
  {
    ignores: [
      "dist",
      "eslint.config.*",
      ".prettierrc.*",
      "postcss.config.cjs",
      "tailwind.config.cjs",
      "vite.config.ts",
      // Vendored RetroUI component source (added via the shadcn registry); kept
      // verbatim so future `shadcn add` updates apply cleanly.
      "src/components/retroui/**",
      "src/lib/utils.ts",
    ],
  },
  js.configs.recommended,
  reactPlugin.configs.flat.recommended,
  ...tseslint.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  importPlugin.flatConfigs.recommended,
  importPlugin.flatConfigs.typescript,
  unicorn.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: {
        project: ["./tsconfig.json", "./tsconfig.node.json"],
        tsconfigRootDir: process.cwd(),
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    settings: {
      react: { version: "detect" },
      jsdoc: { mode: "typescript" },
    },
    rules: {
      "react/react-in-jsx-scope": "off",
      "unicorn/filename-case": "off",
      // The "@/*" path alias is resolved by Vite and type-checked by tsc; the
      // import plugin's default resolver doesn't read tsconfig paths, so this
      // rule produces false positives for those imports.
      "import/no-unresolved": ["error", { ignore: ["^@/"] }],
    },
  },
];
