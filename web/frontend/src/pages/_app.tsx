import type { AppProps } from 'next/app'
import { Inter } from 'next/font/google'
import '@/styles/globals.css'
import { QueryProvider } from '@/components/providers/QueryProvider'
import { AuthProvider } from '@/components/providers/AuthProvider'
import { ConditionalLayout } from '@/components/layout/ConditionalLayout'

const inter = Inter({ subsets: ['latin'] })

export default function MyApp({ Component, pageProps }: AppProps) {
  return (
    <div className={inter.className}>
      <QueryProvider>
        <AuthProvider>
          <ConditionalLayout>
            <Component {...pageProps} />
          </ConditionalLayout>
        </AuthProvider>
      </QueryProvider>
    </div>
  )
}
