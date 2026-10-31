/** 本文件提供 HR 与猎头工具专题的公开页面与独立搜索元数据。 */

import RecruitmentGuidePage from "@/components/RecruitmentGuidePage";
import { HR_TOOLS_GUIDE } from "@/lib/recruitment-guides";
import { createPageMetadata } from "@/lib/seo";

export const metadata = createPageMetadata(HR_TOOLS_GUIDE);

/** HRToolsPage 展示简历整理、猎头推荐与企业招聘流程的不同用法。 */
export default function HRToolsPage() {
  return <RecruitmentGuidePage guide={HR_TOOLS_GUIDE} />;
}
