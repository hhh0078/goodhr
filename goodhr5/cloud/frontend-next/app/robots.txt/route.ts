/** 本文件按请求生成爬虫规则；公开文章的增删由所指向的动态站点地图反映。 */

import { SITE_URL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 返回官网和 AI 爬虫共用的公开范围，排除后台、登录及 API，不暴露任何私有地址。 */
export function GET() {
  const content = `# GoodHR public website
# Published articles are updated automatically in the sitemap.
# AI reading index: ${SITE_URL}/llms.txt
User-agent: *
Allow: /
Disallow: /admin
Disallow: /login
Disallow: /api/

Sitemap: ${SITE_URL}/sitemap.xml
Host: ${SITE_URL}
`;
  return new Response(content, { headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" } });
}
