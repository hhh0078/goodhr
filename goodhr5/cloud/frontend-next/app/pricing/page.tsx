/** 本文件展示官网免费试用与按量充值说明，旧会员套餐仅在文件底部注释保留。 */
import Button from "antd/es/button";
import type { Metadata } from "next";
import Link from "next/link";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { AUTO_REPLY_RELEASE_NOTICE } from "@/lib/recruitment-platforms";
import { absoluteURL, createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR价格 - 免费试用，最低1元起充",
  description: "GoodHR注册即送体验金额，先免费试用，满意再充值。无需购买会员，最低1元起充；部分模型1元可筛选上千份简历，实际消耗因模型与简历内容而异。",
  path: "/pricing",
  keywords: ["GoodHR价格", "招聘软件免费试用", "AI筛选简历费用", "最低1元起充"],
});

/** PricingPage 以文本说明试用、充值和模型费用，不展示或请求会员套餐。 */
export default function PricingPage() {
  return <>
    <StructuredData data={{ "@context": "https://schema.org", "@type": "WebPage", name: "GoodHR免费试用与充值说明", url: absoluteURL("/pricing"), description: "注册即送体验金额，最低1元起充，AI按模型实际用量计费。" }} />
    <MarketingShell eyebrow="免费试用 · 按量充值" title="最低1元起充，满意再充值" description="注册就送体验金额，先让小助手认真干点活。用得顺手，再决定要不要充值。">
      <section className="public-pricing-3">
        <div className="public-container public-pricing-copy">
          <h2 className="public-text">先免费试用，不用买会员</h2>
          <p className="public-text">注册即送体验金额，可以先体验 AI 筛选与招聘流程。使用满意了再充值，<strong>最低1元起充</strong>，按实际用量扣费。</p>
          <h2 className="public-text">1元，也能认真筛简历</h2>
          <p className="public-text"><strong>部分模型1元可筛选上千份简历。</strong>不同模型价格不同，简历内容长度和分析需求也会影响消耗，实际费用以所选模型及真实用量为准。</p>
          <p className="public-text">在后台选择适合自己的模型，再开始筛选。先试一试，满意了再让小助手加班。</p>
          <p className="public-release-notice">{AUTO_REPLY_RELEASE_NOTICE}</p>
          <div className="public-pricing-actions">
            <Button href="/login" type="primary" size="large">注册免费试用</Button>
            <Link href="/download">下载试用</Link>
          </div>
        </div>
      </section>
    </MarketingShell>
  </>;
}

// 旧会员套餐展示暂时停用，原页面代码逐行注释留档，保留以后恢复时需要的套餐与对比逻辑。
// /** 本文件负责官网产品定价页及版本对比内容。 */
//
// import CheckRoundedIcon from "@ant-design/icons/lib/icons/CheckOutlined";
// import CloseRoundedIcon from "@ant-design/icons/lib/icons/CloseOutlined";
// import Button from "antd/es/button";
// import type { Metadata } from "next";
// import MarketingShell from "@/components/MarketingShell";
// import StructuredData from "@/components/StructuredData";
// import { getPublicPlans, type PublicPlanData } from "@/lib/public-data";
// import { AUTO_REPLY_RELEASE_NOTICE } from "@/lib/recruitment-platforms";
// import { absoluteURL, createPageMetadata } from "@/lib/seo";
//
// export const metadata: Metadata = createPageMetadata({
//   title: "GoodHR价格与免费版 - 招聘自动化和AI筛选套餐",
//   description:
//     "GoodHR 关键词筛选、基础招聘岗位运行和自动打招呼可免费使用，AI筛选与详情分析按方案订阅。招聘消息自动回复仍在开发验证，功能开放以正式版本为准。",
//   path: "/pricing",
//   keywords: [
//     "免费招聘自动化工具",
//     "免费自动打招呼软件",
//     "AI招聘软件价格",
//     "猎头软件价格",
//   ],
// });
//
// const comparisons = [
//   ["关键词筛选", true, true, true],
//   ["平台账号与本地程序", true, true, true],
//   ["基础岗位运行和打招呼", true, true, true],
//   ["AI 候选人筛选", false, true, true],
//   ["AI 详情分析", false, true, true],
//   ["自动回复（开发验证中）", false, false, true],
// ] as const;
//
// /** PricingPage 展示免费版、Plus 基础版和 Max 全能版。 */
// export default async function PricingPage() {
//   const remotePlans = await getPublicPlans();
//   const plans = [...remotePlans];
//   return (
//     <>
//       <StructuredData
//         data={{
//           "@context": "https://schema.org",
//           "@type": "Product",
//           name: "GoodHR AI招聘助手",
//           url: absoluteURL("/pricing"),
//           description: "多平台招聘自动筛选、自动打招呼、AI分析和简历管理工具。",
//           offers: plans.map((plan) => ({
//             "@type": "Offer",
//             name: plan.name,
//             price: finalPrice(plan),
//             priceCurrency: "CNY",
//             availability: "https://schema.org/InStock",
//             url: absoluteURL("/pricing"),
//           })),
//         }}
//       />
//       <MarketingShell
//         eyebrow="永久免费 + Plus + Max"
//         title="基础招聘免费用，按需选择 AI 能力"
//         description="关键词筛选和基础打招呼可以免费用，AI 筛选与详情分析按会员方案开放。先看当前能用的功能，再选适合自己的方案。"
//       >
//         <section className="public-pricing-3">
//           <div className="public-container">
//             <p className="public-release-notice">{AUTO_REPLY_RELEASE_NOTICE}</p>
//             <div className="public-pricing-5">
//               {plans.map((plan) => (
//                 <div
//                   key={plan.id}
//                   style={
//                     {
//                       "--public-pricing-6-0":
//                         plan.memberType === "max"
//                           ? "var(--goodhr-primary, #159a62)"
//                           : "var(--goodhr-border, #dce5e0)",
//                       "--public-pricing-6-1":
//                         plan.memberType === "max"
//                           ? "0 18px 48px rgba(21,154,98,.12)"
//                           : "none",
//                     } as import("react").CSSProperties
//                   }
//                   className="public-panel public-pricing-6"
//                 >
//                   <h2 className="public-text public-pricing-7">{plan.name}</h2>
//                   <div className="public-pricing-8">
//                     <p className="public-text public-pricing-9">
//                       ￥{finalPrice(plan)}
//                     </p>
//                     <p className="public-text public-pricing-10">
//                       {planUnit(plan)}
//                     </p>
//                   </div>
//                   {plan.discountAmount > 0 ? (
//                     <p className="public-text public-pricing-11">
//                       原价 ￥{plan.originalPrice}
//                     </p>
//                   ) : null}
//                   <p className="public-text public-pricing-12">
//                     {plan.description}
//                   </p>
//                   <div className="public-pricing-13">
//                     {plan.features.map((feature) => (
//                       <div key={feature} className="public-pricing-14">
//                         <CheckRoundedIcon
//                           className="public-pricing-15"
//                           aria-hidden="true"
//                         />
//                         <p className="public-text">{feature.includes("自动回复") ? `${feature}（开发验证中）` : feature}</p>
//                       </div>
//                     ))}
//                   </div>
//                   <Button
//                     href="/login"
//                     type={plan.memberType === "max" ? "primary" : "default"}
//                     block
//                     className="public-pricing-17"
//                   >
//                     立即使用
//                   </Button>
//                 </div>
//               ))}
//             </div>
//             <div className="public-pricing-18">
//               <h2 className="public-text public-pricing-19">版本功能对比</h2>
//               <div className="public-pricing-20">
//                 <div className="public-pricing-21">
//                   <p className="public-text">功能</p>
//                   <p className="public-text public-pricing-23">免费</p>
//                   <p className="public-text public-pricing-24">Plus</p>
//                   <p className="public-text public-pricing-25">Max</p>
//                 </div>
//                 {comparisons.map(([name, free, plus, max]) => (
//                   <div key={name} className="public-pricing-26">
//                     <p className="public-text">{name}</p>
//                     <div
//                       className="public-pricing-28"
//                       role="img"
//                       aria-label={free ? "包含" : "不包含"}
//                     >
//                       {free ? (
//                         <CheckRoundedIcon
//                           className="public-pricing-29"
//                           aria-hidden="true"
//                         />
//                       ) : (
//                         <CloseRoundedIcon
//                           className="public-pricing-30"
//                           aria-hidden="true"
//                         />
//                       )}
//                     </div>
//                     <div
//                       className="public-pricing-31"
//                       role="img"
//                       aria-label={plus ? "包含" : "不包含"}
//                     >
//                       {plus ? (
//                         <CheckRoundedIcon
//                           className="public-pricing-32"
//                           aria-hidden="true"
//                         />
//                       ) : (
//                         <CloseRoundedIcon
//                           className="public-pricing-33"
//                           aria-hidden="true"
//                         />
//                       )}
//                     </div>
//                     <div
//                       className="public-pricing-34"
//                       role="img"
//                       aria-label={max ? "包含" : "不包含"}
//                     >
//                       {max ? (
//                         <CheckRoundedIcon
//                           className="public-pricing-35"
//                           aria-hidden="true"
//                         />
//                       ) : (
//                         <CloseRoundedIcon
//                           className="public-pricing-36"
//                           aria-hidden="true"
//                         />
//                       )}
//                     </div>
//                   </div>
//                 ))}
//               </div>
//             </div>
//           </div>
//         </section>
//       </MarketingShell>
//     </>
//   );
// }
//
// /** freePlan 返回官网固定展示的免费版本。 */
// function freePlan(): PublicPlanData {
//   return {
//     id: "free",
//     name: "永久免费版",
//     memberType: "free",
//     durationDays: 0,
//     originalPrice: 0,
//     discountAmount: 0,
//     allowAutoReply: false,
//     description: "适合先用关键词规则跑通招聘流程。",
//     features: ["关键词筛选", "基础岗位运行", "平台账号管理", "自动打招呼"],
//   };
// }
//
// /** finalPrice 计算套餐优惠后的展示价格。 */
// function finalPrice(plan: PublicPlanData) {
//   return Math.max(0, plan.originalPrice - plan.discountAmount);
// }
//
// /** planUnit 根据套餐天数返回简短计价单位。 */
// function planUnit(plan: PublicPlanData) {
//   if (!plan.durationDays) return "长期免费";
//   if (plan.durationDays >= 365) return "元 / 年";
//   if (plan.durationDays >= 90) return "元 / 季";
//   return "元 / 月";
// }
