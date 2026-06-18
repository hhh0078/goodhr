/** 本文件负责展示 GoodHR 招聘自动化主流程。 */
import AutoAwesomeRoundedIcon from "@mui/icons-material/AutoAwesomeRounded";
import CalendarMonthRoundedIcon from "@mui/icons-material/CalendarMonthRounded";
import FilterAltRoundedIcon from "@mui/icons-material/FilterAltRounded";
import ForumRoundedIcon from "@mui/icons-material/ForumRounded";
import { Box, Container, Typography } from "@mui/material";

const workflow = [
  {
    icon: AutoAwesomeRoundedIcon,
    index: "01",
    title: "AI分析",
    description: "AI根据岗位模板分析候选人",
  },
  {
    icon: FilterAltRoundedIcon,
    index: "01",
    title: "自动打招呼",
    description: "自动打招呼并持续跟进",
  },

  {
    icon: ForumRoundedIcon,
    index: "03",
    title: "自动回复沟通",
    description: "AI根据设置好的目标，自动跟候选人确认",
  },
  {
    icon: CalendarMonthRoundedIcon,
    index: "04",
    title: "邀约",
    description: "推进高意向候选人面试",
  },
];

/** WorkflowBand 使用轻量横向结构展示招聘流程。 */
export default function WorkflowBand() {
  return (
    <Box
      sx={{
        borderTop: "1px solid",
        borderBottom: "1px solid",
        borderColor: "divider",
        bgcolor: "rgba(255,255,255,0.68)",
        py: { xs: 4, md: 4.5 },
      }}
    >
      <Container maxWidth='lg'>
        <Box
          sx={{
            position: "relative",
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "repeat(4, 1fr)" },
            gap: { xs: 3.5, md: 0 },
          }}
        >
          <Box
            aria-hidden='true'
            sx={{
              position: "absolute",
              left: { xs: 19, md: "12.5%" },
              top: 20,
              bottom: { xs: 20, md: "auto" },
              width: { xs: "2px", md: "75%" },
              height: { xs: "auto", md: "2px" },
              bgcolor: "divider",
            }}
          >
            <Box
              sx={{
                width: "100%",
                height: "100%",
                bgcolor: "primary.main",
                transformOrigin: { xs: "top", md: "left" },
                animation: {
                  xs: "workflowProgressY 5s ease-in-out infinite",
                  md: "workflowProgressX 5s ease-in-out infinite",
                },
              }}
            />
          </Box>
          {workflow.map((item, index) => {
            const Icon = item.icon;
            return (
              <Box
                key={item.title}
                sx={{
                  position: "relative",
                  zIndex: 1,
                  display: "grid",
                  gridTemplateColumns: { xs: "40px 1fr", md: "1fr" },
                  gap: { xs: 1.75, md: 1.5 },
                  px: { md: 2 },
                  textAlign: { md: "center" },
                }}
              >
                <Box
                  sx={{
                    mx: { md: "auto" },
                    width: 40,
                    height: 40,
                    display: "grid",
                    placeItems: "center",
                    color: "#ffffff",
                    bgcolor: "primary.main",
                    border: "5px solid #ffffff",
                    borderRadius: "50%",
                    boxShadow: "0 0 0 1px #dce5e0",
                    animation: "workflowNode 5s ease-in-out infinite",
                    animationDelay: `${index * 1.05}s`,
                  }}
                >
                  <Icon sx={{ fontSize: 18 }} />
                </Box>
                <Box>
                  <Typography
                    sx={{
                      color: "text.primary",
                      fontWeight: 800,
                      fontSize: 17,
                    }}
                  >
                    {item.title}
                  </Typography>
                  <Typography
                    sx={{ mt: 0.5, color: "text.secondary", fontSize: 14 }}
                  >
                    {item.description}
                  </Typography>
                </Box>
              </Box>
            );
          })}
        </Box>
      </Container>
    </Box>
  );
}
