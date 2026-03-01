/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // Twitter/X Brand Colors
        primary: '#1DA1F2',
        secondary: '#14171A',
        accent: '#657786',
        light: '#AAB8C2',
        extraLight: '#E1E8ED',
        extraExtraLight: '#F5F8FA',
      }
    },
  },
  plugins: [],
}
