/** 文件作用说明：仅在确认客服知识库就绪并明确启用后，为公开首页加载GoodHR专用GoodChat客服悬浮窗。 */
import Script from "next/script";

// 知识库、机器人、表单及转人工已在生产验收，并于2026-11-20获准公开。
const CUSTOMER_SUPPORT_READY = true;

/** HomeCustomerSupport 在服务端按发布开关加载专用脚本；默认不向访客加载尚未验收的客服。 */
export default function HomeCustomerSupport() {
  if (!CUSTOMER_SUPPORT_READY) return null;
  return <Script id="goodhr-official-customer-support" src="https://goodchat.58it.cn/api/v1/widget/31add4aef67d4d5d97998aee146bd1a4.js" strategy="afterInteractive" />;
}
