/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        bg:    "#0f1015",
        panel: "#181a22",
        card:  "#1f222c",
        border: "#2a2d39",
        accent: "#7c5cff",
        accent2: "#22d3ee",
        muted: "#8b8f99",
      },
      boxShadow: {
        soft: "0 4px 24px rgba(0,0,0,0.35)",
      },
      borderRadius: {
        xl2: "1rem",
      },
    },
  },
  plugins: [],
};
