/** @type {import('next').NextConfig} */
const nextConfig = {
  // Only use export mode in production
  ...(process.env.NODE_ENV === 'production' && {
    output: 'export',
    trailingSlash: true,
    distDir: '../dist',
    assetPrefix: '',
  }),
  images: {
    unoptimized: true
  },
  eslint: {
    ignoreDuringBuilds: true,
  },
  typescript: {
    ignoreBuildErrors: true,
  },
  // Configure for embedding in Go binary
  generateBuildId: async () => {
    return 'maxiofs-console'
  }
}

module.exports = nextConfig