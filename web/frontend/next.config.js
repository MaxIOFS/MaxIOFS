/** @type {import('next').NextConfig} */
const path = require('path')

const nextConfig = {
  // Disable image optimization
  images: {
    unoptimized: true
  },

  eslint: {
    ignoreDuringBuilds: true,
  },

  typescript: {
    ignoreBuildErrors: true,
  },

  // Disable trailing slashes
  trailingSlash: false,

  // Webpack configuration for path aliases
  webpack: (config) => {
    config.resolve.alias = {
      ...config.resolve.alias,
      '@': path.resolve(__dirname, './src'),
    }
    return config
  },
}

module.exports = nextConfig