/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: [
    './index.html',
    './pages/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    './app/**/*.{ts,tsx}',
    './src/**/*.{ts,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        // Brand colors (Primary Blue)
        brand: {
          50: 'rgb(var(--color-brand-50) / <alpha-value>)',
          100: 'rgb(var(--color-brand-100) / <alpha-value>)',
          200: 'rgb(var(--color-brand-200) / <alpha-value>)',
          300: 'rgb(var(--color-brand-300) / <alpha-value>)',
          400: 'rgb(var(--color-brand-400) / <alpha-value>)',
          500: 'rgb(var(--color-brand-500) / <alpha-value>)',
          600: 'rgb(var(--color-brand-600) / <alpha-value>)',
          700: 'rgb(var(--color-brand-700) / <alpha-value>)',
          800: 'rgb(var(--color-brand-800) / <alpha-value>)',
          900: 'rgb(var(--color-brand-900) / <alpha-value>)',
        },
        // Sidebar colors
        sidebar: {
          bg: 'rgb(var(--color-sidebar-bg) / <alpha-value>)',
          hover: 'rgb(var(--color-sidebar-hover) / <alpha-value>)',
          text: 'rgb(var(--color-sidebar-text) / <alpha-value>)',
          'text-muted': 'rgb(var(--color-sidebar-text-muted) / <alpha-value>)',
          border: 'rgb(var(--color-sidebar-border) / <alpha-value>)',
        },
        // Success colors (Green)
        success: {
          50: 'rgb(var(--color-success-50) / <alpha-value>)',
          100: 'rgb(var(--color-success-100) / <alpha-value>)',
          200: 'rgb(var(--color-success-200) / <alpha-value>)',
          300: 'rgb(var(--color-success-300) / <alpha-value>)',
          400: 'rgb(var(--color-success-400) / <alpha-value>)',
          500: 'rgb(var(--color-success-500) / <alpha-value>)',
          600: 'rgb(var(--color-success-600) / <alpha-value>)',
          700: 'rgb(var(--color-success-700) / <alpha-value>)',
          800: 'rgb(var(--color-success-800) / <alpha-value>)',
        },
        // Error colors (Red)
        error: {
          50: 'rgb(var(--color-error-50) / <alpha-value>)',
          100: 'rgb(var(--color-error-100) / <alpha-value>)',
          200: 'rgb(var(--color-error-200) / <alpha-value>)',
          300: 'rgb(var(--color-error-300) / <alpha-value>)',
          400: 'rgb(var(--color-error-400) / <alpha-value>)',
          500: 'rgb(var(--color-error-500) / <alpha-value>)',
          600: 'rgb(var(--color-error-600) / <alpha-value>)',
          700: 'rgb(var(--color-error-700) / <alpha-value>)',
          800: 'rgb(var(--color-error-800) / <alpha-value>)',
        },
        // Warning colors (Orange/Yellow)
        warning: {
          50: 'rgb(var(--color-warning-50) / <alpha-value>)',
          100: 'rgb(var(--color-warning-100) / <alpha-value>)',
          200: 'rgb(var(--color-warning-200) / <alpha-value>)',
          300: 'rgb(var(--color-warning-300) / <alpha-value>)',
          400: 'rgb(var(--color-warning-400) / <alpha-value>)',
          500: 'rgb(var(--color-warning-500) / <alpha-value>)',
          600: 'rgb(var(--color-warning-600) / <alpha-value>)',
          700: 'rgb(var(--color-warning-700) / <alpha-value>)',
          800: 'rgb(var(--color-warning-800) / <alpha-value>)',
        },
        // Blue Light
        'blue-light': {
          50: 'rgb(var(--color-blue-light-50) / <alpha-value>)',
          100: 'rgb(var(--color-blue-light-100) / <alpha-value>)',
          200: 'rgb(var(--color-blue-light-200) / <alpha-value>)',
          300: 'rgb(var(--color-blue-light-300) / <alpha-value>)',
          400: 'rgb(var(--color-blue-light-400) / <alpha-value>)',
          500: 'rgb(var(--color-blue-light-500) / <alpha-value>)',
          600: 'rgb(var(--color-blue-light-600) / <alpha-value>)',
          700: 'rgb(var(--color-blue-light-700) / <alpha-value>)',
        },
        // Orange
        orange: {
          50: 'rgb(var(--color-orange-50) / <alpha-value>)',
          100: 'rgb(var(--color-orange-100) / <alpha-value>)',
          200: 'rgb(var(--color-orange-200) / <alpha-value>)',
          300: 'rgb(var(--color-orange-300) / <alpha-value>)',
          400: 'rgb(var(--color-orange-400) / <alpha-value>)',
          500: 'rgb(var(--color-orange-500) / <alpha-value>)',
          600: 'rgb(var(--color-orange-600) / <alpha-value>)',
        },
      },
      boxShadow: {
        'card': '0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)',
        'card-hover': '0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)',
      },
    },
  },
  plugins: [],
}
