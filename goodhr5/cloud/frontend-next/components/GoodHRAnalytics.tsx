/** 文件作用说明：仅在常规页面加载统计，含私人分享令牌的页面不加载任何统计脚本。 */
"use client";
import Script from "next/script";
import { usePathname } from "next/navigation";

/** GoodHRAnalytics 在公开简历和诊断分享页屏蔽第三方统计，避免令牌或私人资料被读取。 */
export default function GoodHRAnalytics() {
  const pathname = usePathname();
  if (!pathname || pathname.startsWith("/resumes/share/") || pathname.startsWith("/diagnostics/")) return null;
  return <Script id="baidu-analytics" strategy="afterInteractive">{`var _hmt = _hmt || [];
(function() {
  var hm = document.createElement("script");
  hm.src = "https://hm.baidu.com/hm.js?089e2d5bc4ddf06b7bf5ab053d5b6fe1";
  var s = document.getElementsByTagName("script")[0];
  s.parentNode.insertBefore(hm, s);
})();`}</Script>;
}
