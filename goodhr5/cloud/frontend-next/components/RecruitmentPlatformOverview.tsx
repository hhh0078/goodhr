/** 本文件展示官网统一的平台能力清单，区分各客户端已开放的招聘流程与独立简历导入能力。 */

import { RECRUITMENT_PLATFORM_FACTS } from "@/lib/recruitment-platforms";
import { PLATFORM_GUIDES } from "@/lib/platform-guides";
import Link from "next/link";
import DownOutlined from "@ant-design/icons/lib/icons/DownOutlined";
import PlatformLogo from "./admin/PlatformLogo";

/** RecruitmentPlatformOverview 按平台分别说明正式自动化入口、开发状态与简历导入能力。 */
export default function RecruitmentPlatformOverview() {
  return (
    <ul className="public-platform-list" aria-label="招聘平台与当前能力">
      {RECRUITMENT_PLATFORM_FACTS.map((platform) => {
        const guide = PLATFORM_GUIDES.find((item) => item.platformId === platform.id);
        return <li key={platform.id} className="public-platform-item">
          <div className="public-platform-heading">
            <span className="public-platform-logo" aria-hidden="true"><PlatformLogo platformID={platform.id} size={48} /></span>
            <h3>{platform.name}</h3>
          </div>
          <details className="public-platform-detail">
            <summary>
            <span className="public-platform-status">
              招聘自动化：{platform.automationStatus}
            </span>
            <DownOutlined aria-hidden="true" />
            </summary>
          <p>{platform.automationDescription}</p>
          <p className="public-platform-import">简历导入：{platform.resumeImportDescription}</p>
          </details>
          {guide ? <Link className="public-platform-guide-link" href={guide.path}>
            {platform.name}助手与使用步骤 <span aria-hidden="true">→</span>
          </Link> : null}
        </li>;
      })}
    </ul>
  );
}
