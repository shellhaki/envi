import type { Metadata } from "next";
import Script from "next/script";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";

// One sans for everything. Headings differentiate by weight and tight
// tracking rather than by a second, rounder typeface — that contrast is what
// separates a precision tool from a friendly-startup template.
const inter = Inter({
  variable: "--font-inter",
  subsets: ["latin"],
});

const mono = JetBrains_Mono({
  variable: "--font-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Envi Dashboard",
  description: "Manage projects, environments, secrets, and access.",
  icons: { icon: [{ url: "/favicon-32.png", sizes: "32x32", type: "image/png" }, { url: "/favicon-192.png", sizes: "192x192", type: "image/png" }], apple: "/apple-touch-icon.png" },
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      data-scroll-behavior="smooth"
      suppressHydrationWarning
      className={`${inter.variable} ${mono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col"><Script id="theme" strategy="beforeInteractive">{`try{document.documentElement.dataset.theme=localStorage.getItem('envi_theme')||((matchMedia('(prefers-color-scheme:dark)').matches)?'dark':'light');document.documentElement.dataset.sidebar=localStorage.getItem('envi_sidebar')||'expanded'}catch(e){}`}</Script>{children}</body>
    </html>
  );
}
