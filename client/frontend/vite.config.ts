import { defineConfig } from "vite";

export default defineConfig({
  build: {
    rolldownOptions: {
      onwarn(warning, warn) {
        // This desktop SPA has no React Server Components. Mantine's client
        // boundaries have no meaning here; preserve all other build warnings.
        if (warning.code === "MODULE_LEVEL_DIRECTIVE" && warning.message.includes("use client")) return;
        warn(warning);
      },
    },
  },
});
