/** 本文件负责官网联系我们页面及联系方式展示。 */

import EmailRoundedIcon from "@mui/icons-material/EmailRounded";
import LanguageRoundedIcon from "@mui/icons-material/LanguageRounded";
import PhoneRoundedIcon from "@mui/icons-material/PhoneRounded";
import { Box, Container, Link, Stack, Typography } from "@mui/material";
import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";
import { createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({ title: "联系GoodHR - 招聘自动化与AI招聘工具咨询", description: "联系 GoodHR，咨询 BOSS、猎聘、智联等招聘平台自动化、AI筛选、自动打招呼、自动回复、本地程序安装和订阅问题。", path: "/contact", keywords: ["招聘自动化咨询", "AI招聘工具客服", "BOSS自动打招呼技术支持"] });

const contacts = [
  { icon: PhoneRoundedIcon, label: "手机与微信", value: "17607080935", href: "tel:17607080935", note: "工作日和周末都可以留言。" },
  { icon: EmailRoundedIcon, label: "电子邮箱", value: "1224299352@qq.com", href: "mailto:1224299352@qq.com", note: "可发送问题截图、日志和使用需求。" },
  { icon: LanguageRoundedIcon, label: "官方网站", value: "goodhr5.58it.cn", href: "https://goodhr5.58it.cn", note: "查看产品更新、教程和下载入口。" },
];

/** ContactPage 展示 GoodHR 联系方式。 */
export default function ContactPage() {
  return <MarketingShell eyebrow="联系我们" title="遇到问题，直接找到我们" description="无论是安装、本地连接、AI 配置还是订阅问题，都可以通过下面的方式联系。">
    <Box component="section" sx={{ pb: { xs: 8, md: 12 } }}><Container maxWidth="lg"><Box sx={{ borderTop: "1px solid", borderColor: "divider" }}>
      {contacts.map((item) => { const Icon = item.icon; return <Box key={item.label} sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "220px 1fr" }, gap: 2, py: 4, borderBottom: "1px solid", borderColor: "divider" }}><Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}><Icon color="primary" /><Typography sx={{ fontWeight: 750 }}>{item.label}</Typography></Stack><Box><Link href={item.href} underline="hover" color="text.primary" sx={{ fontSize: { xs: 24, md: 30 }, fontWeight: 760 }}>{item.value}</Link><Typography sx={{ mt: 1, color: "text.secondary" }}>{item.note}</Typography></Box></Box>; })}
    </Box></Container></Box>
  </MarketingShell>;
}
