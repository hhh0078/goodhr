/** 本文件负责展示 GoodHR 新版官网首页。 */
import ArrowForwardRoundedIcon from "@mui/icons-material/ArrowForwardRounded";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import InsightsRoundedIcon from "@mui/icons-material/InsightsRounded";
import SpeedRoundedIcon from "@mui/icons-material/SpeedRounded";
import VerifiedUserRoundedIcon from "@mui/icons-material/VerifiedUserRounded";
import { Box, Button, Container, Stack, Typography } from "@mui/material";
import SiteFooter from "@/components/SiteFooter";
import SiteHeader from "@/components/SiteHeader";
import WorkflowBand from "@/components/WorkflowBand";

const downloadURL =
  "https://ssk8864.oss-cn-shenzhen.aliyuncs.com/GooHR%E5%AE%89%E8%A3%85%E7%A8%8B%E5%BA%8F.exe";

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

/** HomePage 输出新版官网首页。 */
export default function HomePage() {
  return (
    <Box sx={{ minHeight: "100vh", bgcolor: "background.default" }}>
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
                  href={downloadURL}
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
                href={downloadURL}
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
