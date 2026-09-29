import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";
import pluginVue from "eslint-plugin-vue";
import css from "@eslint/css";
import { defineConfig } from "eslint/config";

export default defineConfig([
  { ignores: [".nuxt/**", ".output/**", "node_modules/**", "dist/**", ".data/**"] },
  { files: ["**/*.{js,mjs,cjs,ts,mts,cts,vue}"], plugins: { js }, extends: ["js/recommended"], languageOptions: { globals: globals.browser } },
  tseslint.configs.recommended,
  // Scope the Vue preset to .vue: its rules crash on the CSS AST otherwise.
  { files: ["**/*.vue"], extends: [pluginVue.configs["flat/essential"]] },
  { files: ["**/*.vue"], languageOptions: { parserOptions: { parser: tseslint.parser } } },
  { files: ["**/*.css"], plugins: { css }, language: "css/css", languageOptions: { tolerant: true }, extends: ["css/recommended"],
    // main.css is Tailwind v4 source (@apply, @theme vars, opt-in modern features), not plain CSS.
    rules: { "css/no-invalid-at-rules": "off", "css/no-invalid-properties": "off", "css/use-baseline": "off", "css/no-important": "off" } },
  // Nuxt auto-imports (ref, useFetch, defineNuxtPlugin, ...) are declared in
  // .nuxt types, not as globals; TypeScript already catches undefined names.
  { files: ["**/*.{ts,mts,cts,vue}"], rules: {
    "no-undef": "off",
    // `const { dropped, ...rest } = obj` strips a field on purpose.
    "@typescript-eslint/no-unused-vars": ["error", { ignoreRestSiblings: true }],
  } },
  // Nuxt pages/components are file-named (index.vue, squad.vue); single words are fine.
  { files: ["**/*.vue"], rules: { "vue/multi-word-component-names": "off" } },
]);
