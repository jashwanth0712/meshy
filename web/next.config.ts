import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */
  cacheComponents: true,
  partialPrefetching: true,
  // The pitch deck with live scenes (built from /demo by demo/build.py) lives at /demo.
  async rewrites() {
    return [{ source: "/demo", destination: "/demo.html" }];
  },
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
  async rewrites() {
    return {
      beforeFiles: [{ source: "/", has: [{ type: "host", value: "get.meshy.live" }], destination: "/install.sh" }],
      afterFiles: [],
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
