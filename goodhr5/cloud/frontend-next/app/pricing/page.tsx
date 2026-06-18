/** 本文件负责官网产品定价页及版本对比内容。 */

import CheckRoundedIcon from "@mui/icons-material/CheckRounded";
import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import { Box, Button, Container, Paper, Stack, Typography } from "@mui/material";
import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";

export const metadata: Metadata = { title: "产品定价", description: "GoodHR 关键词筛选永久免费，AI 筛选和智能沟通功能可按需订阅。", alternates: { canonical: "/pricing" } };

const plans = [
  { name: "永久免费版", price: "0", unit: "长期免费", description: "适合先用关键词规则跑通招聘流程。", features: ["关键词筛选", "基础任务", "平台账号管理", "自动打招呼"] },
  { name: "Plus 月度", price: "70", unit: "元 / 月", description: "适合短期集中招聘和 AI 初筛。", features: ["免费版全部功能", "AI 候选人筛选", "AI 详情分析", "自动聊天跟进"] },
  { name: "Plus 季度", price: "180", unit: "元 / 季", description: "适合连续招聘，较月付节省 30 元。", features: ["Plus 全部功能", "连续三个月使用", "自动邀约面试", "优先功能支持"] },
  { name: "Plus 年度", price: "600", unit: "元 / 年", description: "适合长期稳定招聘，较月付节省 240 元。", features: ["Plus 全部功能", "全年持续使用", "团队招聘场景", "更低月均成本"] },
];

const comparisons = [
  ["关键词筛选", true, true], ["平台账号与本地程序", true, true], ["基础任务和打招呼", true, true], ["AI 候选人筛选", false, true], ["AI 详情分析", false, true], ["自动聊天和邀约", false, true],
] as const;

/** PricingPage 展示免费版和 Plus 订阅方案。 */
export default function PricingPage() {
  return <MarketingShell eyebrow="永久免费 + AI 订阅" title="关键词免费用，AI 能力按需升级" description="免费版可以跑完整的基础招聘流程。需要 AI 判断、继续沟通和邀约面试时，再升级 Plus。">
    <Box component="section" sx={{ pb: { xs: 8, md: 12 } }}><Container maxWidth="lg">
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)", lg: "repeat(4, 1fr)" }, gap: 2 }}>
        {plans.map((plan, index) => <Paper key={plan.name} variant="outlined" sx={{ p: 3, borderRadius: "8px", borderColor: index === 2 ? "primary.main" : "divider", boxShadow: index === 2 ? "0 18px 48px rgba(21,154,98,.12)" : "none" }}>
          <Typography component="h2" sx={{ fontSize: 20, fontWeight: 760 }}>{plan.name}</Typography><Stack direction="row" spacing={0.75} sx={{ mt: 2, alignItems: "baseline" }}><Typography sx={{ fontSize: 42, fontWeight: 800 }}>￥{plan.price}</Typography><Typography color="text.secondary">{plan.unit}</Typography></Stack><Typography sx={{ mt: 1.5, minHeight: 52, color: "text.secondary", lineHeight: 1.65 }}>{plan.description}</Typography><Stack spacing={1.25} sx={{ mt: 3 }}>{plan.features.map((feature) => <Stack key={feature} direction="row" spacing={1} sx={{ alignItems: "center" }}><CheckRoundedIcon color="primary" fontSize="small" /><Typography>{feature}</Typography></Stack>)}</Stack><Button component="a" href="/login" variant={index === 2 ? "contained" : "outlined"} fullWidth sx={{ mt: 3 }}>立即使用</Button>
        </Paper>)}
      </Box>
      <Box sx={{ mt: 10 }}><Typography component="h2" sx={{ fontSize: { xs: 30, md: 40 }, fontWeight: 760 }}>版本功能对比</Typography><Box sx={{ mt: 3, borderTop: "1px solid", borderColor: "divider" }}>{comparisons.map(([name, free, plus]) => <Box key={name} sx={{ display: "grid", gridTemplateColumns: "minmax(0,1fr) 120px 120px", py: 2, borderBottom: "1px solid", borderColor: "divider", alignItems: "center" }}><Typography>{name}</Typography><Stack sx={{ alignItems: "center" }}>{free ? <CheckRoundedIcon color="primary" /> : <CloseRoundedIcon color="disabled" />}</Stack><Stack sx={{ alignItems: "center" }}>{plus ? <CheckRoundedIcon color="primary" /> : <CloseRoundedIcon color="disabled" />}</Stack></Box>)}</Box></Box>
    </Container></Box>
  </MarketingShell>;
}
