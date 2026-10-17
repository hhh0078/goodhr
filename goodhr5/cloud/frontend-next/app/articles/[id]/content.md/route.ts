/** 本文件提供单篇已公开文章的完整纯文本 Markdown，不允许读取草稿、待发布文章或私有简历。 */

import { aiArticleMarkdown, aiDiscoveryResponse, unavailableAIDiscovery } from "@/lib/ai-discovery";
import { getPublicArticle } from "@/lib/public-articles";
import { absoluteURL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 沿用公开文章权限读取全文，撤回或删除立即返回 404，接口故障返回可重试的 503。 */
export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  try {
    const article = await getPublicArticle((await params).id);
    if (!article) return aiDiscoveryResponse("文章不存在或尚未公开。\n", 404);
    const response = aiDiscoveryResponse(aiArticleMarkdown(article, absoluteURL));
    response.headers.set("Link", `<${absoluteURL(`/articles/${article.id.toLowerCase()}`)}>; rel="canonical"`);
    return response;
  } catch {
    return unavailableAIDiscovery();
  }
}
