/** 本文件负责官网顶部导航和移动端菜单的客户端交互。 */
"use client";

import { DownOutlined, MenuOutlined } from "@ant-design/icons";
import { Button, Drawer } from "antd";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Fragment, useEffect, useId, useRef, useState } from "react";
import type { PublicStatsData } from "@/lib/public-data";
import BrandMark from "./BrandMark";
import PublicStats from "./PublicStats";
import PublicThemeSwitch from "./PublicThemeSwitch";
import PlatformLogo from "./admin/PlatformLogo";
import { isPublicNavigationActive } from "@/lib/public-appearance";

const navItems = [
  { label: "首页", href: "/" },
  { label: "功能介绍", href: "/features" },
  { label: "定价", href: "/pricing" },
  { label: "视频教程", href: "/videos" },
  { label: "动态", href: "/articles" },
  { label: "下载", href: "/download" },
  { label: "联系作者", href: "/contact" },
];

type PlatformNavigationLink = { label: string; href: string; platformID: string };

/** PlatformNavigation 复用平台链接，桌面支持悬停和键盘，手机通过点击展开。 */
function PlatformNavigation({ links, mobile = false, onNavigate }: {
  links: PlatformNavigationLink[];
  mobile?: boolean;
  onNavigate?: () => void;
}) {
  const pathname = usePathname();
  const [expanded, setExpanded] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const listID = useId();

  // 点击菜单外部时收起，离开组件后及时移除监听。
  useEffect(() => {
    if (!expanded) return;
    /** closeOutside 在点击组件外部时关闭平台列表。 */
    function closeOutside(event: PointerEvent) {
      if (event.target instanceof Node && !container.current?.contains(event.target)) setExpanded(false);
    }
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [expanded]);

  return (
    <div
      ref={container}
      className={`public-platform-navigation${mobile ? " public-platform-navigation-mobile" : ""}`}
      onPointerEnter={(event) => { if (!mobile && event.pointerType !== "touch") setExpanded(true); }}
      onPointerLeave={(event) => {
        if (!mobile && !event.currentTarget.contains(document.activeElement)) setExpanded(false);
      }}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setExpanded(false); }}
      onKeyDown={(event) => {
        if (event.key === "Escape" && expanded) {
          event.stopPropagation();
          setExpanded(false);
          trigger.current?.focus();
        }
      }}
    >
      <button
        ref={trigger}
        type="button"
        className="public-platform-trigger"
        data-active={isPublicNavigationActive(pathname, "/platforms") || undefined}
        aria-expanded={expanded}
        aria-controls={listID}
        onClick={(event) => setExpanded((value) => mobile || event.detail === 0 ? !value : true)}
      >
        适配平台 <DownOutlined aria-hidden="true" />
      </button>
      <div className="public-platform-dropdown" hidden={!expanded}>
        <ul id={listID} className="public-platform-links">
          {links.map((link) => (
            <li key={link.href}>
              <Link href={link.href} aria-current={isPublicNavigationActive(pathname, link.href) ? "page" : undefined} onClick={() => { setExpanded(false); onNavigate?.(); }}>
                <span className="public-platform-logo" aria-hidden="true"><PlatformLogo platformID={link.platformID} size={26} /></span>
                {link.label}
              </Link>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

/** SiteHeaderClient 输出带交互的桌面导航和移动端菜单。 */
export default function SiteHeaderClient({
  stats,
  platformLinks,
}: {
  stats: PublicStatsData;
  platformLinks: PlatformNavigationLink[];
}) {
  const pathname = usePathname();
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
              <Fragment key={item.href}>
                <Link href={item.href} aria-current={isPublicNavigationActive(pathname, item.href) ? pathname === item.href ? "page" : "location" : undefined}>{item.label}</Link>
                {item.href === "/features" && <PlatformNavigation links={platformLinks} />}
              </Fragment>
            ))}
          </nav>
          <div className="public-site-desktop-actions">
            <PublicStats stats={stats} compact />
            <Button href="/login" type="primary">
              进入控制台
            </Button>
          </div>
          <PublicThemeSwitch />
          <Button
            className="public-site-mobile-menu"
            aria-label="打开导航菜单"
            type="text"
            icon={<MenuOutlined aria-hidden="true" />}
            onClick={() => setOpen(true)}
          />
        </div>
        <div className="public-site-mobile-stats"><PublicStats stats={stats} compact mobile /></div>
      </div>
      <Drawer
        rootClassName="public-theme-portal public-site-drawer"
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
              <Fragment key={item.href}>
                <Link href={item.href} aria-current={isPublicNavigationActive(pathname, item.href) ? pathname === item.href ? "page" : "location" : undefined} onClick={closeMenu}>{item.label}</Link>
                {item.href === "/features" && <PlatformNavigation links={platformLinks} mobile onNavigate={closeMenu} />}
              </Fragment>
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
