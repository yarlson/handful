import { defineConfig } from "astro/config";

import tailwind from "@astrojs/tailwind";
import rehypeDocs from "./src/rehype-docs.mjs";

// https://astro.build/config
export default defineConfig({
  site: "https://handful.dev",
  integrations: [tailwind({ applyBaseStyles: false })],
  // The only Markdown on the site is the repo's docs. The code blocks take the site's colours from CSS.
  markdown: { syntaxHighlight: false, rehypePlugins: [rehypeDocs] },
  // The docs are read from the repo root, one level above this folder.
  vite: { server: { fs: { allow: [".."] } } },
});
