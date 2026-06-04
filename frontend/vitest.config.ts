import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    globals: true,
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    // Playwright lives in /e2e; vitest has no business there.
    exclude: ["e2e/**", "node_modules/**"],
    coverage: {
      include: ["src/utils/**", "src/components/CommentFilterButtons.tsx"],
    },
  },
});
