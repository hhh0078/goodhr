/** 本文件负责官网顶部导航和移动端菜单的客户端交互。 */
"use client";

import { MenuOutlined } from "@ant-design/icons";
import { Button, Drawer } from "antd";
import Link from "next/link";
import { useState } from "react";
import type { PublicStatsData } from "@/lib/public-data";
import BrandMark from "./BrandMark";
import PublicStats from "./PublicStats";

const navItems = [
  { label: "首页", href: "/" },
  { label: "功能介绍", href: "/features" },
  { label: "定价", href: "/pricing" },
  { label: "视频教程", href: "/videos" },
  { label: "动态", href: "/articles" },
  { label: "下载", href: "/download" },
  { label: "联系作者", href: "/contact" },
];

/** SiteHeaderClient 输出带交互的桌面导航和移动端菜单。 */
export default function SiteHeaderClient({
  stats,
}: {
  stats: PublicStatsData;
}) {
  const [open, setOpen] = useState(false);

  /** closeMenu 关闭移动端导航。 */
  function closeMenu() {
    setOpen(false);
  }

  return (
    <header className="public-site-header">
      <div className="public-container">
        <div className="public-site-navigation">
          <BrandMark />
          <nav className="public-site-desktop-links" aria-label="官网导航">
            {navItems.map((item) => (
              <Link key={item.href} href={item.href}>
                {item.label}
              </Link>
            ))}
          </nav>
          <div className="public-site-desktop-actions">
            <PublicStats stats={stats} compact />
            <Button href="/login" type="primary">
              进入控制台
            </Button>
          </div>
          <div className="public-site-mobile-stats">
            <PublicStats stats={stats} compact mobile />
          </div>
          <Button
            className="public-site-mobile-menu"
            aria-label="打开导航菜单"
            type="text"
            icon={<MenuOutlined aria-hidden="true" />}
            onClick={() => setOpen(true)}
          />
        </div>
      </div>
      <Drawer
        placement="right"
        open={open}
        onClose={closeMenu}
        title={<BrandMark />}
        size={288}
        closeIcon={<span aria-label="关闭导航菜单">×</span>}
      >
        <div className="public-site-drawer-content">
          <PublicStats stats={stats} compact />
          <nav
            aria-label="移动端官网导航"
            className="public-site-drawer-content"
          >
            {navItems.map((item) => (
              <Link key={item.href} href={item.href} onClick={closeMenu}>
                {item.label}
              </Link>
            ))}
          </nav>
          <Button href="/login" onClick={closeMenu} type="primary" block>
            进入控制台
          </Button>
        </div>
      </Drawer>
    </header>
  );
}
