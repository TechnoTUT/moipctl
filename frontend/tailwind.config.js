import forms from '@tailwindcss/forms'

// The same Tailwind setup and brand colour as moip-toolkit's frontend, so that the two UIs look alike.
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{vue,js,ts}'],
  theme: {
    extend: {
      colors: {
        brand: { DEFAULT: '#C7000A', hover: '#b00009', light: '#e61e29', dim: '#590509' },
      },
    },
  },
  plugins: [forms],
}
