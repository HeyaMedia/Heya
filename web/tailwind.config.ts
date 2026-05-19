import type { Config } from 'tailwindcss'

export default {
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        bg: {
          0: '#07070a',
          1: '#0c0c10',
          2: '#131318',
          3: '#1a1a20',
          4: '#232329',
          5: '#2c2c34',
        },
        fg: {
          0: '#f4f3ee',
          1: '#d6d4cc',
          2: '#8d8a82',
          3: '#5d5b56',
          4: '#3a3936',
        },
        gold: {
          DEFAULT: '#e6b94a',
          bright: '#f3cb66',
          deep: '#b88e2a',
          soft: 'rgba(230, 185, 74, 0.18)',
          glow: 'rgba(230, 185, 74, 0.35)',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'sans-serif'],
        mono: ['JetBrains Mono', 'IBM Plex Mono', 'ui-monospace', 'monospace'],
      },
      borderRadius: {
        xs: '4px',
        sm: '6px',
        md: '10px',
        lg: '14px',
        xl: '20px',
      },
    },
  },
} satisfies Config
