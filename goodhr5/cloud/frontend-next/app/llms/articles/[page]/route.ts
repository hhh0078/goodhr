/** 本文件按页输出已公开文章的 AI 阅读索引，每页最多一千篇，并提供后续页和全文链接。 */

import { aiArticleCatalogMarkdown, aiArticleCatalogPage, aiDiscoveryResponse, assertCompleteAIArticleCatalog, unavailableAIDiscovery } from "@/lib/ai-discovery";
import { ARTICLE_CATALOG_PAGE_SIZE } from "@/lib/article-public-utils";
import { getPublicArticleCatalogPage } from "@/lib/public-articles";
import { absoluteURL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 校验 .txt 页码并输出完整目录，空库首页可读，越界页返回 404。 */
export async function GET(_request: Request, { params }: { params: Promise<{ page: string }> }) {
  const page = aiArticleCatalogPage((await params).page, ARTICLE_CATALOG_PAGE_SIZE);
  if (!page) return aiDiscoveryResponse("文章目录不存在。\n", 404);
  try {
    const catalog = await getPublicArticleCatalogPage(page);
    assertCompleteAIArticleCatalog(catalog, page, ARTICLE_CATALOG_PAGE_SIZE);
    if (page > Math.max(1, Math.ceil(catalog.total / ARTICLE_CATALOG_PAGE_SIZE))) return aiDiscoveryResponse("文章目录不存在。\n", 404);
    return aiDiscoveryResponse(aiArticleCatalogMarkdown(catalog, absoluteURL, "# GoodHR 公开文章目录"));
  } catch {
    return unavailableAIDiscovery();
  }
}
