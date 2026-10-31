/** 本文件提供招聘消息自动回复专题的公开页面与独立搜索元数据。 */

import RecruitmentGuidePage from "@/components/RecruitmentGuidePage";
import { RECRUITMENT_AUTO_REPLY_GUIDE } from "@/lib/recruitment-guides";
import { createPageMetadata } from "@/lib/seo";

export const metadata = createPageMetadata(RECRUITMENT_AUTO_REPLY_GUIDE);

/** RecruitmentAutoReplyPage 展示招聘沟通目标、会员费用和自动回复开放范围。 */
export default function RecruitmentAutoReplyPage() {
  return <RecruitmentGuidePage guide={RECRUITMENT_AUTO_REPLY_GUIDE} />;
}
