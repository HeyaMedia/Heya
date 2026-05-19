import type { Config } from 'tailwindcss'

export default {
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        heya: {
          primary: '#7C6DD8',
          'primary-light': '#9B8FE0',
          movie: '#5B9FE4',
          tv: '#9B7DD4',
          music: '#5BC48C',
          book: '#D4A95B',
        },
        surface: {
          DEFAULT: '#0a0a12',
          raised: '#12121e',
          overlay: '#1a1a2e',
          border: '#2a2a3e',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
      },
    },
  },
} satisfies Config
