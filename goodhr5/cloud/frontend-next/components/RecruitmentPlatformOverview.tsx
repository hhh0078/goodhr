/** 本文件展示官网统一的平台能力清单，避免把简历导入与开发中的招聘自动化混为一谈。 */

import { RECRUITMENT_PLATFORM_FACTS } from "@/lib/recruitment-platforms";

/** RecruitmentPlatformOverview 按平台分别说明正式自动化入口、开发状态与简历导入能力。 */
export default function RecruitmentPlatformOverview() {
  return (
    <ul className="public-platform-list" aria-label="招聘平台与当前能力">
      {RECRUITMENT_PLATFORM_FACTS.map((platform) => (
        <li key={platform.id} className="public-platform-item">
          <div className="public-platform-heading">
            <h3>{platform.name}</h3>
            <span className={platform.automationStatus === "开发中" ? "public-platform-status public-platform-status-pending" : "public-platform-status"}>
              招聘自动化：{platform.automationStatus}
            </span>
          </div>
          <p>{platform.automationDescription}</p>
          <p className="public-platform-import">简历导入：{platform.resumeImportDescription}</p>
        </li>
      ))}
    </ul>
  );
}
