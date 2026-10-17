/** 本文件实时提供 GoodHR 的公开产品事实、官网页面及已发布文章目录，供 AI 搜索和问答读取。 */

import { aiArticleCatalogMarkdown, aiDiscoveryResponse, aiMarkdownText, assertCompleteAIArticleCatalog, unavailableAIDiscovery } from "@/lib/ai-discovery";
import { ARTICLE_CATALOG_PAGE_SIZE } from "@/lib/article-public-utils";
import { BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT } from "@/lib/browser-extension-platforms";
import { getPublicArticleCatalogPage } from "@/lib/public-articles";
import { PUBLIC_SITE_PAGES } from "@/lib/public-site-pages";
import { absoluteURL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 输出可直接阅读的公开事实与实时文章链接，上游故障返回 503 而不是残缺索引。 */
export async function GET() {
  try {
    const catalog = await getPublicArticleCatalogPage(1);
    assertCompleteAIArticleCatalog(catalog, 1, ARTICLE_CATALOG_PAGE_SIZE);
    const content = `# GoodHR

> GoodHR 是面向 HR、招聘团队和猎头顾问的招聘自动化工具，通过本地程序完成招聘平台浏览器操作，帮助整理候选人、筛选简历、招聘沟通与团队协作。

## 核心能力
- 关键词与排除词筛选、AI 简历筛选、候选人评分和详情分析。
- 招聘平台自动打招呼、招聘消息回复和后续沟通。
- AI 批量解析、手动添加和浏览器插件导入简历，按手机号和履历线索查重。
- 浏览器简历插件目前支持${BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT}。
- 结构化简历库、收藏夹、团队协作与简历下载管理。
- 猎头推荐流程按候选人与岗位记录推荐阶段、备注和推进历史。
- 企业 HR 招聘流程通过模板管理面试、面评、审核、Offer 和入职确认。
- 关键词筛选与基础招聘流程可免费使用；会员与 AI 费用以产品定价及用户实际配置为准。

## 简历库、推荐与企业招聘的关系
候选人先进入结构化简历库，猎头按岗位记录推荐进度，企业 HR 再通过独立招聘流程推进面试、评价和录用。简历备注、猎头推荐历史与企业招聘进程分别保存，不会互相覆盖。

## 数据与隐私
招聘平台登录状态、Cookie、浏览器资料、截图和 OCR 数据保存在用户本机。云端负责账号、岗位、运行配置、订阅、团队以及用户导入的结构化简历数据。本文件和下列文章文件仅提供无需登录的官网内容，不提供账号、候选人简历或团队内部资料。

## 官方页面
${PUBLIC_SITE_PAGES.map((page) => `- [${aiMarkdownText(page.title)}](${absoluteURL(page.path)})：${page.description}`).join("\n")}
- [动态站点地图](${absoluteURL("/sitemap.xml")})：公开网页与已发布文章的搜索引擎索引。
- [机器人规则](${absoluteURL("/robots.txt")})：公开页面的爬取规则与站点地图入口。

${aiArticleCatalogMarkdown(catalog, absoluteURL)}`;
    return aiDiscoveryResponse(content);
  } catch {
    return unavailableAIDiscovery();
  }
}
