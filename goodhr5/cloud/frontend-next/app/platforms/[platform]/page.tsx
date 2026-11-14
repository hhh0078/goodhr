/** 本文件为五个已适配招聘平台生成可直接阅读、可被收录的独立官网专题。 */

import { notFound } from "next/navigation";
import RecruitmentGuidePage from "@/components/RecruitmentGuidePage";
import { getPlatformGuide, PLATFORM_GUIDES } from "@/lib/platform-guides";
import { createPageMetadata } from "@/lib/seo";

type PlatformPageProps = { params: Promise<{ platform: string }> };

export const dynamicParams = false;

/** generateStaticParams 仅生成明确维护内容的五个平台地址，不为任意关键词创建重复页面。 */
export function generateStaticParams() {
  return PLATFORM_GUIDES.map(({ slug }) => ({ platform: slug }));
}

/** generateMetadata 为各平台设置独立标题、摘要和指向自身的标准地址，未知平台返回 404。 */
export async function generateMetadata({ params }: PlatformPageProps) {
  const guide = getPlatformGuide((await params).platform);
  if (!guide) notFound();
  return { ...createPageMetadata(guide), title: { absolute: `${guide.title} | GoodHR` } };
}

/** PlatformPage 复用官网专题布局，输出对应平台的招聘用途、操作步骤及真实能力说明。 */
export default async function PlatformPage({ params }: PlatformPageProps) {
  const guide = getPlatformGuide((await params).platform);
  if (!guide) notFound();
  return <RecruitmentGuidePage guide={guide} />;
}
