export default {
  testRunner: "vitest",
  mutate: ["src/lib/**/*.ts", "!src/lib/**/*.test.ts"],
  coverageAnalysis: "perTest",
  reporters: ["clear-text", "progress"],
  // Stryker rewrites tsconfig paths with the TypeScript 6 compiler API, which
  // TypeScript 7 does not have. No tsconfig here points outside web/, so this
  // names a file that does not exist, and Stryker skips the rewrite.
  tsconfigFile: "no-tsconfig-rewrite.json",
}
