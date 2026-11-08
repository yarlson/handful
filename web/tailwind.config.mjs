import defaultTheme from "tailwindcss/defaultTheme";

/** @type {import('tailwindcss').Config} */
export default {
  future: {
    hoverOnlyWhenSupported: true,
  },
  content: ["./src/**/*.{astro,html,js,jsx,md,mdx,svelte,ts,tsx,vue}"],
  theme: {
    extend: {
      colors: {
        band: "var(--band)",
        ink: {
          DEFAULT: "var(--ink)",
          2: "var(--ink-2)",
          3: "var(--ink-3)",
        },
        line: {
          DEFAULT: "var(--line)",
          strong: "var(--line-strong)",
        },
        hover: "var(--hover)",
        pressed: "var(--pressed)",
        fill: "var(--fill)",
        button: {
          DEFAULT: "var(--button)",
          hover: "var(--button-hover)",
          ink: "var(--button-ink)",
        },
      },
      fontFamily: {
        sans: ['"Geist Variable"', ...defaultTheme.fontFamily.sans],
      },
    },
  },
  plugins: [],
};
