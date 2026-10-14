/** 文件作用说明：保留招聘设置旧地址，复用企业招聘弹框同一套公司、流程和邮件设置内容。 */
"use client";
import RecruitmentSettingsContent from "@/components/admin/recruitment/RecruitmentSettingsContent";

/** RecruitmentSettingsPage 保留直接访问兼容，所有设置行为由共用组件维护。 */
export default function RecruitmentSettingsPage() {
  return <RecruitmentSettingsContent />;
}