import { Html, Head, Main, NextScript } from 'next/document'

export default function Document() {
  return (
    <Html lang="en">
      <Head>
        <link rel="icon" href="/assets/img/icon.png" />
        <link rel="apple-touch-icon" href="/assets/img/icon.png" />
        <meta name="description" content="High-Performance S3-Compatible Object Storage Management Console" />
      </Head>
      <body>
        <Main />
        <NextScript />
      </body>
    </Html>
  )
}
