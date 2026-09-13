import type { Metadata } from "next";
import Script from "next/script";
import { Inter, JetBrains_Mono } from "next/font/google";
import DocsShell from "@/components/docs-shell";
import "./globals.css";

const inter = Inter({ variable: "--font-inter", subsets: ["latin"] });
const mono = JetBrains_Mono({ variable: "--font-mono", subsets: ["latin"] });

export const metadata: Metadata = {
  title: { default: "Envi Docs", template: "%s · Envi Docs" },
  description: "Environment secrets, encrypted, scoped, and audited.",
  icons: {
    icon: [
      { url: "/favicon-32.png", sizes: "32x32", type: "image/png" },
      { url: "/favicon-192.png", sizes: "192x192", type: "image/png" },
    ],
    apple: "/apple-touch-icon.png",
  },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" data-scroll-behavior="smooth" suppressHydrationWarning className={`${inter.variable} ${mono.variable}`}>
      <body>
        <Script id="theme" strategy="beforeInteractive">
          {`try{document.documentElement.dataset.theme=localStorage.getItem('envi_theme')||((matchMedia('(prefers-color-scheme:dark)').matches)?'dark':'light')}catch(e){}`}
        </Script>
        <DocsShell>{children}</DocsShell>
      </body>
    </html>
  );
}
