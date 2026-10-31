/** 本文件提供多平台招聘自动化专题的公开页面与独立搜索元数据。 */

import RecruitmentGuidePage from "@/components/RecruitmentGuidePage";
import { RECRUITMENT_AUTOMATION_GUIDE } from "@/lib/recruitment-guides";
import { createPageMetadata } from "@/lib/seo";

export const metadata = createPageMetadata(RECRUITMENT_AUTOMATION_GUIDE);

/** RecruitmentAutomationPage 展示候选人筛选、自动打招呼和平台能力边界。 */
export default function RecruitmentAutomationPage() {
  return <RecruitmentGuidePage guide={RECRUITMENT_AUTOMATION_GUIDE} />;
}
