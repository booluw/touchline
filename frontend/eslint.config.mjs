// Minimal flat ESLint config for the Nuxt 3 SPA (S03-02). The repository's
// package manager is pnpm; install with `pnpm install`, run with `pnpm run lint`.
// Rules: JavaScript recommended + TypeScript recommended + Vue essential.
// `no-undef` is off: Nuxt auto-imports (ref, navigateTo, useAuth, ...) are
// resolved by the framework, and vue-tsc type-checks real undefineds.
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  {
    ignores: ['.nuxt/**', '.output/**', 'dist/**', 'coverage/**', 'node_modules/**', '*.config.*'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/essential'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        extraFileExtensions: ['.vue'],
        sourceType: 'module',
      },
    },
  },
  {
    rules: {
      'no-undef': 'off',
      'vue/multi-word-component-names': 'off',
    },
  },
)