/** 本文件负责展示 GoodHR 新版邮箱验证码登录页面。 */
import ArrowBackRoundedIcon from "@mui/icons-material/ArrowBackRounded";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import { Box, Button, Container, Paper, Stack, Typography } from "@mui/material";
import BrandMark from "@/components/BrandMark";
import LoginForm from "@/components/LoginForm";

export const dynamic = "force-dynamic";

const loginPoints = ["邮箱验证码登录，无需记密码", "登录状态与现有后台完全兼容", "平台账号与浏览器数据仍保留在本地"];

/** LoginPage 输出与新版首页统一的明亮登录界面。 */
export default function LoginPage() {
  return (
    <Box sx={{ minHeight: "100vh", bgcolor: "background.default", display: "flex", flexDirection: "column" }}>
      <Container maxWidth="lg" sx={{ py: 2.5 }}>
        <Stack direction="row" sx={{ alignItems: "center", justifyContent: "space-between" }}>
          <BrandMark />
          <Button component="a" href="/" color="secondary" startIcon={<ArrowBackRoundedIcon />}>返回首页</Button>
        </Stack>
      </Container>
      <Container maxWidth="lg" sx={{ flex: 1, display: "grid", alignItems: "center", py: { xs: 5, md: 8 } }}>
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "minmax(0, 1fr) 460px" }, gap: { xs: 6, md: 12 }, alignItems: "center" }}>
          <Box>
            <Typography sx={{ color: "primary.main", fontWeight: 800, fontSize: 14 }}>GOODHR 控制台</Typography>
            <Typography component="h1" sx={{ mt: 2, maxWidth: 620, color: "text.primary", fontSize: { xs: 42, sm: 54, md: 64 }, lineHeight: 1.12, fontWeight: 780 }}>
              登录之后，继续你的招聘任务
            </Typography>
            <Typography sx={{ mt: 3, maxWidth: 590, color: "text.secondary", fontSize: 18, lineHeight: 1.8 }}>
              账号和岗位信息保存在云端，招聘平台登录状态、截图和浏览器数据只留在你的电脑里。
            </Typography>
            <Stack spacing={1.5} sx={{ mt: 4 }}>
              {loginPoints.map((point) => (
                <Stack key={point} direction="row" spacing={1.25} sx={{ alignItems: "center" }}>
                  <CheckCircleRoundedIcon color="primary" fontSize="small" />
                  <Typography sx={{ color: "text.secondary" }}>{point}</Typography>
                </Stack>
              ))}
            </Stack>
          </Box>
          <Paper variant="outlined" sx={{ p: { xs: 3, sm: 4 }, borderRadius: "24px", borderColor: "divider", boxShadow: "0 24px 70px rgba(31, 55, 43, 0.10)" }}>
            <Typography component="h2" sx={{ color: "text.primary", fontSize: 28, fontWeight: 750 }}>欢迎回来</Typography>
            <Typography sx={{ mt: 1, mb: 3.5, color: "text.secondary" }}>输入邮箱，获取 4 位验证码</Typography>
            <LoginForm />
          </Paper>
        </Box>
      </Container>
    </Box>
  );
}
