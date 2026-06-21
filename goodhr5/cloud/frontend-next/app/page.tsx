/** 本文件负责展示 GoodHR 新版官网首页。 */
import ArrowForwardRoundedIcon from "@mui/icons-material/ArrowForwardRounded";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import InsightsRoundedIcon from "@mui/icons-material/InsightsRounded";
import MarkChatReadRoundedIcon from "@mui/icons-material/MarkChatReadRounded";
import PersonSearchRoundedIcon from "@mui/icons-material/PersonSearchRounded";
import SaveAltRoundedIcon from "@mui/icons-material/SaveAltRounded";
import SpeedRoundedIcon from "@mui/icons-material/SpeedRounded";
import VerifiedUserRoundedIcon from "@mui/icons-material/VerifiedUserRounded";
import { Box, Button, Container, Stack, Typography } from "@mui/material";
import type { Metadata } from "next";
import SiteFooter from "@/components/SiteFooter";
import SiteHeader from "@/components/SiteHeader";
import StructuredData from "@/components/StructuredData";
import WorkflowBand from "@/components/WorkflowBand";
import { absoluteURL, createPageMetadata, RECRUITMENT_PLATFORMS } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR AI招聘助手 - 自动筛选、自动打招呼与招聘消息回复",
  description: "GoodHR 帮助 HR 和猎头在主流招聘平台完成自动筛选简历、AI筛选、自动打招呼、AI自动回复、候选人跟进和简历下载管理，关键词模式与基础流程可免费使用。",
  path: "/",
  keywords: ["免费招聘软件", "免费自动打招呼", "猎头AI工具", "HR自动化招聘", "多招聘平台自动化"],
});

const benefits = [
  {
    icon: SpeedRoundedIcon,
    title: "减少重复操作",
    description: "连续读取候选人、筛选并执行沟通，让招聘人员专注判断。",
  },
  {
    icon: InsightsRoundedIcon,
    title: "结果清楚可追踪",
    description: "每次分析都有分数和理由，任务处理数量随时可见。",
  },
  {
    icon: VerifiedUserRoundedIcon,
    title: "动作更接近人工",
    description: "控制节奏、滚动和操作间隔，避免机械式批量点击。",
  },
];

const automationScenes = [
  { icon: PersonSearchRoundedIcon, title: "自动筛选与 AI 筛选", description: "按关键词、排除词和岗位要求处理候选人，也可以通过 AI 完成简历评分、详情分析和筛选理由说明。" },
  { icon: SpeedRoundedIcon, title: "自动打招呼与 AI 打招呼", description: "根据岗位模板和筛选结果自动执行打招呼，减少 HR 与猎头逐个打开候选人、重复点击的时间。" },
  { icon: MarkChatReadRoundedIcon, title: "招聘消息自动回复", description: "围绕岗位要求继续沟通，支持 AI 自动回复、候选人意向确认、关键信息收集和面试邀约场景。" },
  { icon: SaveAltRoundedIcon, title: "简历下载与人才库整理", description: "整理候选人详情、评分与沟通结果，帮助管理 BOSS 简历下载、猎聘简历下载和跨平台候选人资料。" },
];

const faqs = [
  { question: "GoodHR 是什么？", answer: "GoodHR 是面向 HR、招聘团队和猎头顾问的招聘自动化工具，用于候选人筛选、AI 分析、自动打招呼、招聘消息回复和简历管理。" },
  { question: "GoodHR 可以免费使用吗？", answer: "可以。关键词筛选、排除词筛选、基础招聘任务和自动打招呼等基础流程可以免费使用，AI 筛选和 AI 详情分析按会员方案使用。" },
  { question: "GoodHR 面向哪些招聘平台？", answer: `GoodHR 面向 ${RECRUITMENT_PLATFORMS.join("、")} 等招聘平台持续适配。` },
  { question: "招聘平台登录信息会上传吗？", answer: "不会。招聘平台 Cookie、浏览器资料、截图和 OCR 数据保存在用户本机，云端主要保存账号认证、岗位、任务和团队配置。" },
];

/** HomePage 输出新版官网首页。 */
export default function HomePage() {
  return (
    <Box sx={{ minHeight: "100vh", bgcolor: "background.default" }}>
      <StructuredData data={[
        { "@context": "https://schema.org", "@type": "SoftwareApplication", name: "GoodHR", applicationCategory: "BusinessApplication", operatingSystem: "Windows, macOS", url: absoluteURL("/"), downloadUrl: absoluteURL("/download"), description: "面向 HR 和猎头的招聘平台自动化工具，支持自动筛选、AI筛选、自动打招呼、AI自动回复和简历管理。", offers: { "@type": "Offer", price: "0", priceCurrency: "CNY", description: "关键词筛选与基础招聘流程可免费使用" }, featureList: automationScenes.map((item) => item.title) },
        { "@context": "https://schema.org", "@type": "FAQPage", mainEntity: faqs.map((item) => ({ "@type": "Question", name: item.question, acceptedAnswer: { "@type": "Answer", text: item.answer } })) },
      ]} />
      <SiteHeader />
      <Box component='main'>
        <Box
          component='section'
          sx={{ pt: { xs: 8, md: 12 }, pb: { xs: 7, md: 10 } }}
        >
          <Container maxWidth='lg'>
            <Box sx={{ maxWidth: 940 }}>
              <Stack
                direction='row'
                spacing={0.75}
                sx={{
                  mb: 3,
                  width: "fit-content",
                  alignItems: "center",
                  color: "primary.main",
                  border: "1px solid",
                  borderColor: "primary.main",
                  borderRadius: "6px",
                  px: 1.25,
                  py: 0.75,
                  bgcolor: "rgba(255,255,255,0.72)",
                }}
              >
                <CheckCircleRoundedIcon sx={{ fontSize: 17 }} />
                <Typography sx={{ fontSize: 13, fontWeight: 800 }}>
                  关键词筛选永久免费
                </Typography>
              </Stack>
              <Typography
                component='h1'
                sx={{
                  maxWidth: 900,
                  color: "text.primary",
                  fontSize: { xs: 46, sm: 58, md: 74 },
                  lineHeight: { xs: 1.12, md: 1.08 },
                  fontWeight: 780,
                }}
              >
                把重复招聘交给 GoodHR，
                <Box component='span' sx={{ color: "primary.main" }}>
                  把时间留给人
                </Box>
              </Typography>
              <Typography
                sx={{
                  mt: 3,
                  maxWidth: 720,
                  color: "text.secondary",
                  fontSize: { xs: 17, md: 20 },
                  lineHeight: 1.8,
                }}
              >
                自动读取候选人，结合岗位模板完成筛选、分析、打招呼和后续跟进。流程持续运转，判断始终清楚可见。
              </Typography>

              <Typography sx={{ mt: 1.5, color: "primary.dark", fontSize: 15, fontWeight: 700 }}>
                HR 和猎头的免费自动招聘助手
              </Typography>
              <Stack
                direction={{ xs: "column", sm: "row" }}
                spacing={1.5}
                sx={{ mt: 4, alignItems: { sm: "center" } }}
              >
                <Button
                  component='a'
                  href='/download'
                  variant='contained'
                  size='large'
                  startIcon={<DownloadRoundedIcon />}
                  sx={{ px: 3 }}
                >
                  免费下载
                </Button>
                <Button
                  component='a'
                  href='/login'
                  variant='outlined'
                  color='secondary'
                  size='large'
                  endIcon={<ArrowForwardRoundedIcon />}
                  sx={{ px: 3 }}
                >
                  进入控制台
                </Button>
              </Stack>
            </Box>
          </Container>
        </Box>

        <WorkflowBand />

        <Box component='section' sx={{ py: { xs: 7, md: 10 }, bgcolor: "#f6f9f7", borderTop: "1px solid", borderBottom: "1px solid", borderColor: "divider" }}>
          <Container maxWidth='lg'>
            <Typography sx={{ color: "primary.main", fontWeight: 800, fontSize: 14 }}>主流招聘平台持续适配</Typography>
            <Typography component='h2' sx={{ mt: 1.5, maxWidth: 820, fontSize: { xs: 32, md: 46 }, lineHeight: 1.2 }}>一个 GoodHR，承接不同招聘平台的重复工作</Typography>
            <Typography sx={{ mt: 2, maxWidth: 780, color: "text.secondary", lineHeight: 1.8 }}>围绕不同招聘平台的候选人列表、详情页、沟通和简历场景持续扩展，统一使用岗位模板、筛选规则和本地浏览器资料。</Typography>
            <Box sx={{ mt: 4, display: "flex", flexWrap: "wrap", gap: 1 }}>{RECRUITMENT_PLATFORMS.map((platform) => <Typography key={platform} component='span' sx={{ px: 1.5, py: 1, border: "1px solid", borderColor: "divider", borderRadius: "6px", bgcolor: "#fff", fontWeight: 700 }}>{platform}</Typography>)}</Box>
          </Container>
        </Box>

        <Box component='section' sx={{ py: { xs: 8, md: 12 }, bgcolor: "#fff" }}>
          <Container maxWidth='lg'>
            <Typography sx={{ color: "primary.main", fontWeight: 800, fontSize: 14 }}>从寻找候选人到发起沟通</Typography>
            <Typography component='h2' sx={{ mt: 1.5, maxWidth: 820, fontSize: { xs: 34, md: 48 }, lineHeight: 1.18 }}>招聘自动化，不只是批量点击</Typography>
            <Box sx={{ mt: 6, display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" }, borderTop: "1px solid", borderColor: "divider" }}>{automationScenes.map((item, index) => { const Icon = item.icon; return <Box key={item.title} sx={{ py: 4, pr: { md: index % 2 === 0 ? 4 : 0 }, pl: { md: index % 2 === 1 ? 4 : 0 }, borderRight: { md: index % 2 === 0 ? "1px solid" : "none" }, borderBottom: "1px solid", borderColor: "divider" }}><Icon color='primary' /><Typography component='h3' sx={{ mt: 1.5, fontSize: 22, fontWeight: 760 }}>{item.title}</Typography><Typography sx={{ mt: 1.25, color: "text.secondary", lineHeight: 1.8 }}>{item.description}</Typography></Box>; })}</Box>
          </Container>
        </Box>

        <Box
          component='section'
          id='capabilities'
          sx={{ py: { xs: 8, md: 12 }, bgcolor: "#ffffff" }}
        >
          <Container maxWidth='lg'>
            <Typography
              sx={{ color: "primary.main", fontWeight: 800, fontSize: 14 }}
            >
              围绕真实招聘流程
            </Typography>
            <Typography
              component='h2'
              sx={{
                mt: 1.5,
                maxWidth: 760,
                color: "text.primary",
                fontSize: { xs: 34, md: 48 },
                lineHeight: 1.18,
              }}
            >
              不增加复杂工作台，只减少每天重复的动作
            </Typography>
            <Box
              sx={{
                mt: 6,
                display: "grid",
                gridTemplateColumns: { xs: "1fr", md: "repeat(3, 1fr)" },
                borderTop: "1px solid",
                borderColor: "divider",
              }}
            >
              {benefits.map((item, index) => {
                const Icon = item.icon;
                return (
                  <Box
                    key={item.title}
                    sx={{
                      py: 4,
                      pr: { md: 4 },
                      pl: { md: index === 0 ? 0 : 4 },
                      borderRight: {
                        md: index < benefits.length - 1 ? "1px solid" : "none",
                      },
                      borderBottom: {
                        xs: index < benefits.length - 1 ? "1px solid" : "none",
                        md: "none",
                      },
                      borderColor: "divider",
                    }}
                  >
                    <Icon color='primary' />
                    <Typography
                      component='h3'
                      sx={{ mt: 2, color: "text.primary", fontSize: 21 }}
                    >
                      {item.title}
                    </Typography>
                    <Typography
                      sx={{ mt: 1.5, color: "text.secondary", lineHeight: 1.8 }}
                    >
                      {item.description}
                    </Typography>
                  </Box>
                );
              })}
            </Box>
          </Container>
        </Box>

        <Box component='section' sx={{ py: { xs: 8, md: 10 }, bgcolor: "#fff" }}>
          <Container maxWidth='lg'>
            <Typography sx={{ color: "primary.main", fontWeight: 800, fontSize: 14 }}>常见问题</Typography>
            <Typography component='h2' sx={{ mt: 1.5, fontSize: { xs: 32, md: 44 } }}>HR 和猎头最关心的问题</Typography>
            <Box sx={{ mt: 4, borderTop: "1px solid", borderColor: "divider" }}>{faqs.map((item) => <Box key={item.question} component='article' sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "minmax(240px,.7fr) minmax(0,1.3fr)" }, gap: 2, py: 3, borderBottom: "1px solid", borderColor: "divider" }}><Typography component='h3' sx={{ fontSize: 18, fontWeight: 760 }}>{item.question}</Typography><Typography sx={{ color: "text.secondary", lineHeight: 1.8 }}>{item.answer}</Typography></Box>)}</Box>
          </Container>
        </Box>

        <Box
          component='section'
          sx={{
            py: { xs: 8, md: 10 },
            bgcolor: "#edf5f0",
            borderTop: "1px solid",
            borderColor: "divider",
          }}
        >
          <Container maxWidth='lg'>
            <Stack
              direction={{ xs: "column", md: "row" }}
              spacing={3}
              sx={{
                alignItems: { md: "center" },
                justifyContent: "space-between",
              }}
            >
              <Box>
                <Typography
                  component='h2'
                  sx={{ color: "text.primary", fontSize: { xs: 32, md: 42 } }}
                >
                  从今天开始，少做一点重复工作
                </Typography>
                <Typography
                  sx={{ mt: 1.5, color: "text.secondary", fontSize: 17 }}
                >
                  关键词筛选和基础任务可以长期免费使用。
                </Typography>
              </Box>
              <Button
                component='a'
                href='/download'
                variant='contained'
                size='large'
                startIcon={<DownloadRoundedIcon />}
                sx={{ flexShrink: 0, px: 3 }}
              >
                下载 GoodHR
              </Button>
            </Stack>
          </Container>
        </Box>
      </Box>
      <SiteFooter />
    </Box>
  );
}
