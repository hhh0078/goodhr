/** 本文件负责配置 GoodHR 新版 Next.js 前端的构建行为。 */
import type { NextConfig } from "next";
import { PUBLIC_SITE_PAGES } from "./lib/public-site-pages";

const staticExport = process.env.GOODHR_STATIC_EXPORT === "1";

const nextConfig: NextConfig = {
  output: staticExport ? "export" : "standalone",
  poweredByHeader: false,
  images: staticExport ? { unoptimized: true } : undefined,
  ...(staticExport
    ? {}
    : {
        /** rewrites 复用现有/api反代，把WorkBuddy公开发现、授权与MCP地址转到云端。 */
        async rewrites() {
          const api = (process.env.NEXT_PUBLIC_CLOUD_API_BASE || (process.env.NODE_ENV === "production" ? "https://goodhr.58it.cn" : "http://127.0.0.1:8084")).replace(/\/$/, "");
          return [
            { source: "/.well-known/oauth-protected-resource", destination: `${api}/api/workbuddy/oauth/protected-resource` },
            { source: "/.well-known/oauth-authorization-server", destination: `${api}/api/workbuddy/oauth/authorization-server` },
            { source: "/oauth/:action(register|authorize|token)", destination: `${api}/api/workbuddy/oauth/:action` },
            { source: "/mcp", destination: `${api}/api/workbuddy/mcp` },
          ];
        },
        /** redirects 修复历史官网入口，并仅统一公开页面的域名和 HTTPS，保留旧客户端使用的后台与 API。 */
        async redirects() {
          const legacyPages = ["features", "pricing", "videos", "download", "contact"].map((name) => ({ source: `/${name}.html`, destination: `/${name}`, permanent: true }));
          const publicPaths = [...new Set([...PUBLIC_SITE_PAGES.map((page) => page.path), "/articles/:path*", "/sitemaps/:path*", "/llms/:path*", "/robots.txt", "/sitemap.xml", "/llms.txt"])];
          return [
            ...legacyPages,
            { source: "/installation.html", destination: "/download", permanent: true },
            ...publicPaths.map((source) => ({
              source,
              has: [{ type: "host" as const, value: "goodhr5\\.58it\\.cn" }],
              destination: `https://goodhr.58it.cn${source}`,
              permanent: true,
            })),
            ...publicPaths.map((source) => ({
              source,
              has: [
                { type: "host" as const, value: "goodhr\\.58it\\.cn" },
                { type: "header" as const, key: "x-forwarded-proto", value: "http" },
              ],
              destination: `https://goodhr.58it.cn${source}`,
              permanent: true,
            })),
          ];
        },
      }),
};

export default nextConfig;
