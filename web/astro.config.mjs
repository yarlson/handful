import { defineConfig } from "astro/config";

import tailwind from "@astrojs/tailwind";

// https://astro.build/config
export default defineConfig({
  site: "https://yargpt.com",
  integrations: [tailwind({ applyBaseStyles: false })],
});
