import Link from "next/link";

export function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="container footer-inner">
        <p>
          © {new Date().getFullYear()} GoodHR | 备案号：
          <a href="https://beian.miit.gov.cn/" target="_blank" rel="noreferrer">
            湘ICP备2021016833号-2
          </a>
        </p>
        <div className="footer-links">
          <Link href="/guide">安装指南</Link>
          <Link href="/tutorial/free">免费版教程</Link>
          <Link href="/tutorial/ai">AI版教程</Link>
          <Link href="/faq">常见问题</Link>
          <Link href="/updates">更新记录</Link>
          <a href="https://58it.cn" target="_blank" rel="noreferrer">
            联系作者
          </a>
        </div>
      </div>
    </footer>
  );
}
