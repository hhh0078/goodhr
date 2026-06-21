/** 本文件负责官网产品定价页及版本对比内容。 */

import CheckRoundedIcon from "@mui/icons-material/CheckRounded";
import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import {
  Box,
  Button,
  Container,
  Paper,
  Stack,
  Typography,
} from "@mui/material";
import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { getPublicPlans, type PublicPlanData } from "@/lib/public-data";
import { absoluteURL, createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR价格与免费版 - 招聘自动化和AI筛选套餐",
  description:
    "GoodHR 关键词筛选、基础招聘任务和自动打招呼可免费使用；AI筛选简历、AI详情分析和招聘消息智能回复可按需订阅。",
  path: "/pricing",
  keywords: [
    "免费招聘自动化工具",
    "免费自动打招呼软件",
    "AI招聘软件价格",
    "猎头软件价格",
  ],
});

const comparisons = [
  ["关键词筛选", true, true],
  ["平台账号与本地程序", true, true],
  ["基础任务和打招呼", true, true],
  ["AI 候选人筛选", false, true],
  ["AI 详情分析", false, true],
  ["自动聊天和邀约", false, true],
] as const;

/** PricingPage 展示免费版和 Plus 订阅方案。 */
export default async function PricingPage() {
  const remotePlans = await getPublicPlans();
  const plans = [...remotePlans];
  return (
    <>
      <StructuredData
        data={{
          "@context": "https://schema.org",
          "@type": "Product",
          name: "GoodHR AI招聘助手",
          url: absoluteURL("/pricing"),
          description: "招聘平台自动筛选、自动打招呼、AI分析和自动回复工具。",
          offers: plans.map((plan) => ({
            "@type": "Offer",
            name: plan.name,
            price: finalPrice(plan),
            priceCurrency: "CNY",
            availability: "https://schema.org/InStock",
            url: absoluteURL("/pricing"),
          })),
        }}
      />
      <MarketingShell
        eyebrow='永久免费 + AI 订阅'
        title='关键词免费用，AI 能力按需升级'
        description='免费版可以跑完整的基础招聘流程。需要 AI 判断、继续沟通和邀约面试时，再升级 Plus。'
      >
        <Box component='section' sx={{ pb: { xs: 8, md: 12 } }}>
          <Container maxWidth='lg'>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: {
                  xs: "1fr",
                  md: "repeat(2, 1fr)",
                  lg: "repeat(4, 1fr)",
                },
                gap: 2,
              }}
            >
              {plans.map((plan, index) => (
                <Paper
                  key={plan.id}
                  variant='outlined'
                  sx={{
                    p: 3,
                    borderRadius: "8px",
                    borderColor: index === 1 ? "primary.main" : "divider",
                    boxShadow:
                      index === 1 ? "0 18px 48px rgba(21,154,98,.12)" : "none",
                  }}
                >
                  <Typography
                    component='h2'
                    sx={{ fontSize: 20, fontWeight: 760 }}
                  >
                    {plan.name}
                  </Typography>
                  <Stack
                    direction='row'
                    spacing={0.75}
                    sx={{ mt: 2, alignItems: "baseline" }}
                  >
                    <Typography sx={{ fontSize: 42, fontWeight: 800 }}>
                      ￥{finalPrice(plan)}
                    </Typography>
                    <Typography color='text.secondary'>
                      {planUnit(plan)}
                    </Typography>
                  </Stack>
                  {plan.discountAmount > 0 ? (
                    <Typography
                      sx={{
                        mt: 0.5,
                        color: "text.secondary",
                        textDecoration: "line-through",
                      }}
                    >
                      原价 ￥{plan.originalPrice}
                    </Typography>
                  ) : null}
                  <Typography
                    sx={{
                      mt: 1.5,
                      minHeight: 52,
                      color: "text.secondary",
                      lineHeight: 1.65,
                    }}
                  >
                    {plan.description}
                  </Typography>
                  <Stack spacing={1.25} sx={{ mt: 3 }}>
                    {plan.features.map((feature) => (
                      <Stack
                        key={feature}
                        direction='row'
                        spacing={1}
                        sx={{ alignItems: "center" }}
                      >
                        <CheckRoundedIcon color='primary' fontSize='small' />
                        <Typography>{feature}</Typography>
                      </Stack>
                    ))}
                  </Stack>
                  <Button
                    component='a'
                    href='/login'
                    variant={index === 1 ? "contained" : "outlined"}
                    fullWidth
                    sx={{ mt: 3 }}
                  >
                    立即使用
                  </Button>
                </Paper>
              ))}
            </Box>
            <Box sx={{ mt: 10 }}>
              <Typography
                component='h2'
                sx={{ fontSize: { xs: 30, md: 40 }, fontWeight: 760 }}
              >
                版本功能对比
              </Typography>
              <Box
                sx={{ mt: 3, borderTop: "1px solid", borderColor: "divider" }}
              >
                {comparisons.map(([name, free, plus]) => (
                  <Box
                    key={name}
                    sx={{
                      display: "grid",
                      gridTemplateColumns: "minmax(0,1fr) 120px 120px",
                      py: 2,
                      borderBottom: "1px solid",
                      borderColor: "divider",
                      alignItems: "center",
                    }}
                  >
                    <Typography>{name}</Typography>
                    <Stack sx={{ alignItems: "center" }}>
                      {free ? (
                        <CheckRoundedIcon color='primary' />
                      ) : (
                        <CloseRoundedIcon color='disabled' />
                      )}
                    </Stack>
                    <Stack sx={{ alignItems: "center" }}>
                      {plus ? (
                        <CheckRoundedIcon color='primary' />
                      ) : (
                        <CloseRoundedIcon color='disabled' />
                      )}
                    </Stack>
                  </Box>
                ))}
              </Box>
            </Box>
          </Container>
        </Box>
      </MarketingShell>
    </>
  );
}

/** freePlan 返回官网固定展示的免费版本。 */
function freePlan(): PublicPlanData {
  return {
    id: "free",
    name: "永久免费版",
    memberType: "free",
    durationDays: 0,
    originalPrice: 0,
    discountAmount: 0,
    description: "适合先用关键词规则跑通招聘流程。",
    features: ["关键词筛选", "基础任务", "平台账号管理", "自动打招呼"],
  };
}

/** finalPrice 计算套餐优惠后的展示价格。 */
function finalPrice(plan: PublicPlanData) {
  return Math.max(0, plan.originalPrice - plan.discountAmount);
}

/** planUnit 根据套餐天数返回简短计价单位。 */
function planUnit(plan: PublicPlanData) {
  if (!plan.durationDays) return "长期免费";
  if (plan.durationDays >= 365) return "元 / 年";
  if (plan.durationDays >= 90) return "元 / 季";
  return "元 / 月";
}
