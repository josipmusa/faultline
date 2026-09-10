import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'Faultline',
  description: 'See every outbound call, and degrade it on purpose.',
};

// System fonts rather than next/font: the build produces a self-contained
// binary, and fetching a webfont at build time makes that build need a network.
export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className="h-full font-sans antialiased">{children}</body>
    </html>
  );
}
