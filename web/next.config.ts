import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  // meshy.live hosts (docs/DNS.md): get.meshy.live/ is the installer,
  // www.meshy.live redirects to the apex.
  // /demo is the pitch presentation with live scenes (web/public/demo.html, built by demo/build.py).
  async rewrites() {
    return {
      beforeFiles: [{ source: "/", has: [{ type: "host", value: "get.meshy.live" }], destination: "/install.sh" }],
      afterFiles: [{ source: "/demo", destination: "/demo.html" }],
      fallback: [],
    };
  },
  async redirects() {
    return [{ source: "/:path*", has: [{ type: "host", value: "www.meshy.live" }], destination: "https://meshy.live/:path*", permanent: true }];
  },
  async headers() {
    return [
      {
        source: "/",
        has: [{ type: "host", value: "get.meshy.live" }],
        headers: [{ key: "Content-Type", value: "text/plain; charset=utf-8" }],
      },
      {
        source: "/install.sh",
        headers: [
          { key: "Content-Type", value: "text/plain; charset=utf-8" },
          { key: "Cache-Control", value: "public, max-age=300" },
        ],
      },
    ];
  },
};

export default nextConfig;
