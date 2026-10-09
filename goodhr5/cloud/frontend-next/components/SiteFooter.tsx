/** 本文件负责展示新版官网底部信息。 */

import Link from "next/link";
import BrandMark from "./BrandMark";

const footerLinks = [
  { label: "功能介绍", href: "/features" },
  { label: "产品定价", href: "/pricing" },
  { label: "视频教程", href: "/videos" },
  { label: "下载", href: "/download" },
  { label: "联系我们", href: "/contact" },
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
              AI筛选、自动打招呼、自动回复和招聘简历管理工具
            </p>
          </div>
          <nav className="public-sitefooter-7">
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
