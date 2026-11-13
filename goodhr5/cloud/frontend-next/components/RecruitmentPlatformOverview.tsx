/** 本文件展示官网统一的平台能力清单，区分各客户端已开放的招聘流程与独立简历导入能力。 */

import { RECRUITMENT_PLATFORM_FACTS } from "@/lib/recruitment-platforms";
import { PLATFORM_GUIDES } from "@/lib/platform-guides";
import Link from "next/link";

/** RecruitmentPlatformOverview 按平台分别说明正式自动化入口、开发状态与简历导入能力。 */
export default function RecruitmentPlatformOverview() {
  return (
    <ul className="public-platform-list" aria-label="招聘平台与当前能力">
      {RECRUITMENT_PLATFORM_FACTS.map((platform) => {
        const guide = PLATFORM_GUIDES.find((item) => item.platformId === platform.id);
        return <li key={platform.id} className="public-platform-item">
          <div className="public-platform-heading">
            <h3>{platform.name}</h3>
            <span className="public-platform-status">
              招聘自动化：{platform.automationStatus}
            </span>
          </div>
          <p>{platform.automationDescription}</p>
          <p className="public-platform-import">简历导入：{platform.resumeImportDescription}</p>
          {guide ? <Link className="public-platform-guide-link" href={guide.path}>
            {platform.name}助手与使用步骤 <span aria-hidden="true">→</span>
          </Link> : null}
        </li>;
      })}
    </ul>
  );
}
