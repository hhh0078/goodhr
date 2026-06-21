/** 本文件负责向 AI 搜索与问答系统提供 GoodHR 的纯文本产品说明。 */

import { PLATFORM_AUTOMATION_KEYWORDS, RECRUITMENT_PLATFORMS, SITE_URL } from "@/lib/seo";

export const dynamic = "force-static";

/** GET 返回便于大模型读取的站点事实、能力和公开页面索引。 */
export function GET() {
  const content = `# GoodHR

> GoodHR 是面向 HR、招聘团队和猎头顾问的招聘自动化工具，通过本地程序完成招聘平台浏览器操作，通过岗位模板完成候选人筛选、详情分析、自动打招呼和后续沟通。

## 核心能力
- 招聘平台候选人自动读取与去重
- 关键词筛选和排除词筛选
- AI 简历筛选、AI 候选人评分和 AI 详情分析
- 自动打招呼、AI 打招呼、自动回复招聘消息和邀约跟进
- 招聘简历整理、结构化简历库和简历下载管理
- 多账号、岗位模板、任务日志和本地浏览器资料管理
- 关键词筛选与基础招聘流程可免费使用

## 招聘平台相关场景
GoodHR 面向 ${RECRUITMENT_PLATFORMS.join("、")} 等招聘平台持续适配。相关场景包括自动打招呼、AI 自动打招呼、自动筛选、AI 筛选、自动回复、AI 自动回复和招聘简历下载。

## 平台与自动化检索词
${PLATFORM_AUTOMATION_KEYWORDS.join("、")}

## 数据与隐私
招聘平台登录状态、Cookie、浏览器资料、截图和 OCR 数据保存在用户本机。云端负责账号认证、岗位和任务配置、订阅与团队数据。

## 官方页面
- 首页：${SITE_URL}/
- 功能介绍：${SITE_URL}/features
- 产品定价：${SITE_URL}/pricing
- 视频教程：${SITE_URL}/videos
- 下载：${SITE_URL}/download
- 联系方式：${SITE_URL}/contact
`;
  return new Response(content, { headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "public, max-age=3600" } });
}
