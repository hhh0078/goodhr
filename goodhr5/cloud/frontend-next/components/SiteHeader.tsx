/** 本文件负责在服务端读取公开统计并输出官网导航。 */

import { getPublicStats } from "@/lib/public-data";
import { PLATFORM_GUIDES } from "@/lib/platform-guides";
import { RECRUITMENT_PLATFORM_FACTS } from "@/lib/recruitment-platforms";
import SiteHeaderClient from "./SiteHeaderClient";

/** SiteHeader 在服务端准备导航数据，避免浏览器请求公开统计接口。 */
export default async function SiteHeader() {
  const stats = await getPublicStats();
  const platformLinks = PLATFORM_GUIDES.map((guide) => ({
    href: guide.path,
    platformID: guide.platformId,
    label: RECRUITMENT_PLATFORM_FACTS.find((platform) => platform.id === guide.platformId)?.name || guide.eyebrow,
  }));
  return <SiteHeaderClient stats={stats} platformLinks={platformLinks} />;
}
