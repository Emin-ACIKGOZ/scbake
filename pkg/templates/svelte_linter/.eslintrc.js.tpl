module.exports = {
  root: true,
  parserOptions: {
    ecmaVersion: 2020,
    sourceType: 'module',
  },
  env: {
    browser: true,
    es2017: true,
    node: true,
  },
  extends: [
    // Use the Svelte default config
    'eslint:recommended',
    '@sveltejs/eslint-config/recommended',
    'prettier',
  ],
  rules: {
    // Add custom project rules here
  },
  overrides: [
    {
      files: ['**/*.svelte'],
      processor: 'svelte3/svelte3',
    },
  ],
  plugins: ['svelte3'],
};