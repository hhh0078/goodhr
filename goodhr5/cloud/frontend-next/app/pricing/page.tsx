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
    "GoodHR 关键词筛选、基础招聘岗位运行和自动打招呼可免费使用；AI筛选简历、AI详情分析和招聘消息智能回复可按需订阅。",
  path: "/pricing",
  keywords: [
    "免费招聘自动化工具",
    "免费自动打招呼软件",
    "AI招聘软件价格",
    "猎头软件价格",
  ],
});

const comparisons = [
  ["关键词筛选", true, true, true],
  ["平台账号与本地程序", true, true, true],
  ["基础岗位运行和打招呼", true, true, true],
  ["AI 候选人筛选", false, true, true],
  ["AI 详情分析", false, true, true],
  ["自动回复", false, false, true],
] as const;

/** PricingPage 展示免费版、Plus 基础版和 Max 全能版。 */
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
        eyebrow='永久免费 + Plus + Max'
        title='基础招聘免费用，自动回复放进 Max'
        description='Plus 基础包月版包含 AI 筛选和自动打招呼；Max 全能包年版再开放自动回复。'
      >
        <Box component='section' sx={{ pb: { xs: 8, md: 12 } }}>
          <Container maxWidth='lg'>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: {
                  xs: "1fr",
                  md: "repeat(2, 1fr)",
                  lg: "repeat(3, 1fr)",
                },
                gap: 2,
              }}
            >
              {plans.map((plan) => (
                <Paper
                  key={plan.id}
                  variant='outlined'
                  sx={{
                    p: 3,
                    borderRadius: "8px",
                    borderColor: plan.memberType === "max" ? "primary.main" : "divider",
                    boxShadow:
                      plan.memberType === "max" ? "0 18px 48px rgba(21,154,98,.12)" : "none",
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
                    variant={plan.memberType === "max" ? "contained" : "outlined"}
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
                <Box
                  sx={{
                    display: "grid",
                    gridTemplateColumns: "minmax(0,1fr) 100px 100px 100px",
                    py: 1.5,
                    borderBottom: "1px solid",
                    borderColor: "divider",
                    fontWeight: 760,
                  }}
                >
                  <Typography>功能</Typography>
                  <Typography sx={{ textAlign: "center" }}>免费</Typography>
                  <Typography sx={{ textAlign: "center" }}>Plus</Typography>
                  <Typography sx={{ textAlign: "center" }}>Max</Typography>
                </Box>
                {comparisons.map(([name, free, plus, max]) => (
                  <Box
                    key={name}
                    sx={{
                      display: "grid",
                      gridTemplateColumns: "minmax(0,1fr) 100px 100px 100px",
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
                    <Stack sx={{ alignItems: "center" }}>
                      {max ? (
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
    allowAutoReply: false,
    description: "适合先用关键词规则跑通招聘流程。",
    features: ["关键词筛选", "基础岗位运行", "平台账号管理", "自动打招呼"],
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
