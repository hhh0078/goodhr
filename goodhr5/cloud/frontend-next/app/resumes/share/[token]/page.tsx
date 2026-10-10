/** 文件作用说明：免登录简历分享页的元信息，禁止索引和转发带令牌的来源地址。 */
import type { Metadata } from "next";
import ResumeShareView from "./share-view";

export const metadata: Metadata = {
  title: "简历分享", robots: { index: false, follow: false, googleBot: { index: false, follow: false } }, referrer: "no-referrer",
};

/** ResumeSharePage 显示经过服务端白名单处理的简历，公开内容不依赖后台登录框架。 */
export default function ResumeSharePage() { return <ResumeShareView />; }
