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

  // API rewrite to backend (development only)
  async rewrites() {
    if (process.env.NODE_ENV === 'development') {
      return [
        {
          source: '/api/v1/:path*',
          destination: 'http://localhost:8081/api/v1/:path*',
        },
      ]
    }
    return []
  },

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