import Link from "next/link";
import type { Route } from "next";

const navItems: { href: Route; label: string }[] = [
  { href: "/", label: "首页" },
  { href: "/guide", label: "安装步骤" },
  { href: "/tutorial/free", label: "免费版教程" },
  { href: "/tutorial/ai", label: "AI版教程" },
  { href: "/features", label: "功能介绍" },
  { href: "/faq", label: "FAQ" },
  { href: "/updates", label: "更新记录" },
];

export function SiteHeader() {
  return (
    <header className="site-header">
      <div className="container header-inner">
        <Link href="/" className="brand">
          GoodHR
        </Link>
        <nav className="nav">
          {navItems.map((item) => (
            <Link key={item.href} href={item.href} className="nav-link">
              {item.label}
            </Link>
          ))}
        </nav>
      </div>
    </header>
  );
}
