/** 本文件负责配置 GoodHR 新版 Next.js 前端的构建行为。 */
import type { NextConfig } from "next";

const staticExport = process.env.GOODHR_STATIC_EXPORT === "1";

const nextConfig: NextConfig = {
  output: staticExport ? "export" : "standalone",
  poweredByHeader: false,
  images: staticExport ? { unoptimized: true } : undefined,
  ...(staticExport
    ? {}
    : {
        async redirects() {
          return ["features", "pricing", "videos", "download", "contact"].map(
            (name) => ({
              source: `/${name}.html`,
              destination: `/${name}`,
              permanent: true,
            }),
          );
        },
      }),
  async headers() {
    return [
      {
        source:
          "/((?!_next/static|favicon.ico|robots.txt|sitemap.xml|llms.txt).*)",
        headers: [
          {
            key: "Cache-Control",
            value: "no-store, no-cache, must-revalidate, proxy-revalidate",
          },
        ],
      },
    ];
  },
};

export default nextConfig;
