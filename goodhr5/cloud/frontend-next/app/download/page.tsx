/** 本文件负责官网本地程序下载页面。 */

import AppleIcon from "@mui/icons-material/Apple";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import WindowRoundedIcon from "@mui/icons-material/WindowRounded";
import { Box, Button, Container, Paper, Stack, Typography } from "@mui/material";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import MarketingShell from "@/components/MarketingShell";

export const metadata: Metadata = { title: "下载 GoodHR", description: "下载 GoodHR Windows 本地程序，安装后通过默认浏览器进入本地控制台。", alternates: { canonical: "/download" } };

const windowsURL = process.env.GOODHR_WINDOWS_DOWNLOAD_URL || "https://ssk8864.oss-cn-shenzhen.aliyuncs.com/GooHR%E5%AE%89%E8%A3%85%E7%A8%8B%E5%BA%8F.exe";
const macURL = process.env.GOODHR_MAC_DOWNLOAD_URL || "";

/** DownloadPage 提供 Windows 和 macOS 本地程序下载入口。 */
export default function DownloadPage() {
  return <MarketingShell eyebrow="本地程序" title="下载并安装 GoodHR" description="本地程序负责浏览器操作、截图、OCR 和本地数据管理。安装后会自动打开控制台。">
    <Box component="section" sx={{ pb: { xs: 8, md: 12 } }}><Container maxWidth="lg">
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" }, gap: 2 }}>
        <DownloadCard icon={<WindowRoundedIcon />} system="Windows" note="支持 Windows 10 / 11，推荐 64 位系统。" href={windowsURL} available />
        <DownloadCard icon={<AppleIcon />} system="macOS" note="适用于 Apple 芯片和 Intel 芯片电脑。" href={macURL} available={Boolean(macURL)} />
      </Box>
      <Box sx={{ mt: 8, borderTop: "1px solid", borderColor: "divider", pt: 4 }}><Typography component="h2" sx={{ fontSize: 28, fontWeight: 760 }}>安装前说明</Typography><Stack spacing={1.5} sx={{ mt: 2.5 }}>{["安装时请勾选创建桌面快捷方式。", "首次启动会检查浏览器、Node 和 OCR 等运行组件。", "招聘平台登录状态、截图和浏览器资料只保存在当前电脑。"].map((text) => <Stack key={text} direction="row" spacing={1} sx={{ alignItems: "center" }}><CheckCircleRoundedIcon color="primary" fontSize="small" /><Typography color="text.secondary">{text}</Typography></Stack>)}</Stack></Box>
    </Container></Box>
  </MarketingShell>;
}

/** DownloadCard 展示一个操作系统的下载入口。 */
function DownloadCard({ icon, system, note, href, available }: { icon: ReactNode; system: string; note: string; href: string; available: boolean }) {
  return <Paper variant="outlined" sx={{ p: { xs: 3, md: 4 }, borderRadius: "8px", borderColor: "divider" }}><Box sx={{ color: "primary.main", "& svg": { fontSize: 34 } }}>{icon}</Box><Typography component="h2" sx={{ mt: 2, fontSize: 28, fontWeight: 760 }}>{system}</Typography><Typography sx={{ mt: 1, minHeight: 48, color: "text.secondary" }}>{note}</Typography>{available ? <Button component="a" href={href} variant="contained" startIcon={<DownloadRoundedIcon />} sx={{ mt: 3 }}>下载安装程序</Button> : <Button disabled variant="outlined" sx={{ mt: 3 }}>正在准备</Button>}</Paper>;
}
