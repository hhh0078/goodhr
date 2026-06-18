/** 本文件负责展示 GoodHR 招聘自动化主流程。 */
import AutoAwesomeRoundedIcon from "@mui/icons-material/AutoAwesomeRounded";
import CalendarMonthRoundedIcon from "@mui/icons-material/CalendarMonthRounded";
import FilterAltRoundedIcon from "@mui/icons-material/FilterAltRounded";
import ForumRoundedIcon from "@mui/icons-material/ForumRounded";
import { Box, Container, Stack, Typography } from "@mui/material";

const workflow = [
  { icon: FilterAltRoundedIcon, index: "01", title: "筛选", description: "按关键词或 AI 判断匹配度" },
  { icon: AutoAwesomeRoundedIcon, index: "02", title: "分析", description: "读取详情并生成清晰理由" },
  { icon: ForumRoundedIcon, index: "03", title: "沟通", description: "自动打招呼并持续跟进" },
  { icon: CalendarMonthRoundedIcon, index: "04", title: "邀约", description: "推进高意向候选人面试" },
];

/** WorkflowBand 使用轻量横向结构展示招聘流程。 */
export default function WorkflowBand() {
  return (
    <Box sx={{ borderTop: "1px solid", borderBottom: "1px solid", borderColor: "divider", bgcolor: "rgba(255,255,255,0.68)" }}>
      <Container maxWidth="lg">
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, 1fr)", md: "repeat(4, 1fr)" } }}>
          {workflow.map((item, index) => {
            const Icon = item.icon;
            return (
              <Stack
                key={item.title}
                direction="row"
                spacing={1.5}
                sx={{
                  py: 3,
                  px: { xs: 0, sm: 2 },
                  borderRight: { md: index < workflow.length - 1 ? "1px solid" : "none" },
                  borderBottom: { xs: index < workflow.length - 1 ? "1px solid" : "none", md: "none" },
                  borderColor: "divider",
                }}
              >
                <Box sx={{ color: "primary.main", animation: index === 1 ? "signalFlow 2.4s ease-in-out infinite" : "none" }}>
                  <Icon />
                </Box>
                <Box>
                  <Typography sx={{ color: "primary.main", fontSize: 12, fontWeight: 800 }}>{item.index}</Typography>
                  <Typography sx={{ color: "text.primary", fontWeight: 800, fontSize: 17 }}>{item.title}</Typography>
                  <Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 14 }}>{item.description}</Typography>
                </Box>
              </Stack>
            );
          })}
        </Box>
      </Container>
    </Box>
  );
}
