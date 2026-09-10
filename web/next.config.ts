import type { NextConfig } from 'next';

// A static export is what makes the UI embeddable: `next build` writes plain
// files to out/, which the Go binary carries and serves itself. There is no
// Node process at runtime, so nothing here may depend on a server: no
// rewrites, no image optimisation, no route handlers.
const nextConfig: NextConfig = {
  output: 'export',
  images: { unoptimized: true },
};

export default nextConfig;
