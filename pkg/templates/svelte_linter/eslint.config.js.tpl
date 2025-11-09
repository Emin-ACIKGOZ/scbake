import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';

/** @type {import('eslint').Linter.Config[]} */
export default [
    // Apply recommended standard JS rules
    js.configs.recommended,
    // Apply recommended Svelte rules
    ...svelte.configs['flat/recommended'],
    {
        languageOptions: {
            // Enable browser and node global variables (e.g., 'window', 'process')
            globals: {
                ...globals.browser,
                ...globals.node
            }
        }
    },
    {
        // Ignore standard build output directories
        ignores: ['dist/', '.svelte-kit/', 'build/']
    }
];