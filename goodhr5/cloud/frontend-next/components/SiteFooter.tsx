/** 本文件负责展示新版官网底部信息。 */

import Link from "next/link";
import BrandMark from "./BrandMark";

const footerLinks = [
  { label: "功能介绍", href: "/features" },
  { label: "多平台招聘", href: "/recruitment-automation" },
  { label: "自动回复进度", href: "/recruitment-auto-reply" },
  { label: "HR与猎头工具", href: "/hr-tools" },
  { label: "定价", href: "/pricing" },
  { label: "视频教程", href: "/videos" },
  { label: "下载", href: "/download" },
  { label: "联系作者", href: "/contact" },
];

/** SiteFooter 输出品牌、联系信息和版权说明。 */
export default function SiteFooter() {
  return (
    <footer className="public-sitefooter-1">
      <div className="public-container">
        <div className="public-sitefooter-3">
          <div>
            <BrandMark />
            <p className="public-text public-sitefooter-6">
              多平台AI筛选、自动打招呼与招聘简历管理工具
            </p>
          </div>
          <nav className="public-sitefooter-7" aria-label="官网与使用指南">
            {footerLinks.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                style={{ color: "inherit", textDecoration: "none" }}
              >
                <p className="public-text public-sitefooter-9">{item.label}</p>
              </Link>
            ))}
          </nav>
          <p className="public-text public-sitefooter-10">联系：17607080935</p>
        </div>
      </div>
    </footer>
  );
}
