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
    },
  },
];
