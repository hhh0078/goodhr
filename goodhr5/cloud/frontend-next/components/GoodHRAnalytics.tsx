/** 文件作用：保留百度统计，在官网和登录页接入Clarity，并排除后台回放与私人分享页面。 */
"use client";
import Script from "next/script";
import { usePathname } from "next/navigation";
import { useLayoutEffect, useRef } from "react";
import { canRecordClarity, CLARITY_PROJECT_ID, isPrivateAnalyticsPath, trackConversion } from "@/lib/conversion-analytics";

/** GoodHRAnalytics 在公开简历、面试邀请和诊断分享页屏蔽第三方统计，避免令牌或私人资料被读取。 */
export default function GoodHRAnalytics() {
  const pathname = usePathname();
  const stopped = useRef(false);
  useLayoutEffect(() => {
    if (!pathname) return;
    if (!canRecordClarity(pathname)) {
      try { if (window.clarity) { window.clarity("stop"); stopped.current = true; } } catch { /* 停止回放不影响页面。 */ }
      return;
    }
    // 本机测试不向第三方发送数据；正式域名沿用同一个Clarity项目。
    if (!["goodhr.58it.cn", "goodhr5.58it.cn"].includes(window.location.hostname)) return;
    try {
      if (stopped.current) { window.clarity?.("start"); stopped.current = false; }
      if (!document.getElementById("goodhr-clarity")) {
        window.clarity ||= Object.assign((...args: unknown[]) => { window.clarity?.q?.push(args); }, { q: [] as unknown[][] });
        const script = document.createElement("script");
        script.id = "goodhr-clarity";
        script.async = true;
        script.src = "https://www.clarity.ms/tag/" + CLARITY_PROJECT_ID;
        script.onload = () => {
          if (!canRecordClarity(window.location.pathname)) window.clarity?.("stop");
        };
        script.onerror = () => script.remove();
        document.head.appendChild(script);
      }
      trackConversion(pathname === "/login" ? "login_view" : "site_visit");
    } catch { /* 统计被浏览器或扩展拦截时，不影响页面和登录。 */ }
  }, [pathname]);
  if (!pathname || isPrivateAnalyticsPath(pathname)) return null;
  return <Script id="baidu-analytics" strategy="afterInteractive">{`var _hmt = _hmt || [];
(function() {
  var hm = document.createElement("script");
  hm.src = "https://hm.baidu.com/hm.js?089e2d5bc4ddf06b7bf5ab053d5b6fe1";
  var s = document.getElementsByTagName("script")[0];
  s.parentNode.insertBefore(hm, s);
})();`}</Script>;
}
