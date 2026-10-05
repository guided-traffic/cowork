import { defineConfig } from 'vitest/config';

// Merged by the Angular unit-test builder into its own Vitest settings (angular.json
// runnerConfig). Vitest's five seconds a test is too short for a component test on a shared CI
// runner under coverage: one that renders a page took 5.4 s there on 2026-10-05, and failed a
// pull request that was otherwise green.
export default defineConfig({
  test: {
    testTimeout: 15_000,
  },
});
