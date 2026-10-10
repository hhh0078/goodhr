/** 文件作用说明：为受限面试邀请关闭搜索索引和来源地址传递，保护邀请令牌与候选人资料。 */
import type { Metadata } from "next";
import type { ReactNode } from "react";

export const metadata: Metadata = {
  title: "面试邀请",
  robots: { index: false, follow: false, googleBot: { index: false, follow: false } },
  referrer: "no-referrer",
};

/** InterviewInvitationLayout 让免登录邀请页复用隐私元信息，不改变候选人的答复流程。 */
export default function InterviewInvitationLayout({ children }: { children: ReactNode }) {
  return <>{children}</>;
}
