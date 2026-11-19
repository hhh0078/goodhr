/** 文件作用说明：仅在确认客服知识库就绪并明确启用后，为公开首页加载GoodHR专用GoodChat客服悬浮窗。 */
import Script from "next/script";

// 知识库和机器人尚待确认，当前发布仅放友链；确认后再显式启用并发布。
const CUSTOMER_SUPPORT_READY = false;

/** HomeCustomerSupport 在服务端按发布开关加载专用脚本；默认不向访客加载尚未验收的客服。 */
export default function HomeCustomerSupport() {
  if (!CUSTOMER_SUPPORT_READY) return null;
  return <Script id="goodhr-official-customer-support" src="https://goodchat.58it.cn/api/v1/widget/31add4aef67d4d5d97998aee146bd1a4.js" strategy="afterInteractive" />;
}
