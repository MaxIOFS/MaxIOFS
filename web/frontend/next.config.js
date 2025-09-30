/** @type {import('next').NextConfig} */
const nextConfig = {
  // Disable export mode to support dynamic routes during development
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