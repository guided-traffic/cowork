// @ts-check
const eslint = require('@eslint/js');
const { defineConfig } = require('eslint/config');
const tseslint = require('typescript-eslint');
const angular = require('angular-eslint');

module.exports = defineConfig([
  // The generated API client (docs/adr/0046 D3) is never edited, so it is not linted.
  { ignores: ['src/app/api/**'] },
  {
    files: ['**/*.ts'],
    extends: [
      eslint.configs.recommended,
      tseslint.configs.recommended,
      tseslint.configs.stylistic,
      angular.configs.tsRecommended,
    ],
    processor: angular.processInlineTemplates,
    rules: {
      '@angular-eslint/directive-selector': [
        'error',
        {
          type: 'attribute',
          prefix: 'app',
          style: 'camelCase',
        },
      ],
      '@angular-eslint/component-selector': [
        'error',
        {
          type: 'element',
          prefix: 'app',
          style: 'kebab-case',
        },
      ],
    },
  },
  {
    files: ['**/*.html'],
    extends: [angular.configs.templateRecommended, angular.configs.templateAccessibility],
    rules: {
      // These PrimeNG components render a native input, which a wrapping label names. A
      // p-select renders a combobox span instead: it is named with ariaLabelledBy, not wrapped.
      '@angular-eslint/template/label-has-associated-control': [
        'error',
        { controlComponents: ['p-password', 'p-checkbox', 'p-toggleswitch'] },
      ],
    },
  },
]);
