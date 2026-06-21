/** 本文件负责展示服务端准备好的官网公开统计数据。 */

import { Box, Stack, Typography } from "@mui/material";
import type { PublicStatsData } from "@/lib/public-data";

/** PublicStats 展示已处理简历和今日新注册数量。 */
export default function PublicStats({ stats, compact = false, mobile = false }: { stats: PublicStatsData; compact?: boolean; mobile?: boolean }) {
  return <Stack direction="row" spacing={mobile ? 0.75 : compact ? 1.5 : 3} sx={{ alignItems: "center" }}>
    <StatValue label="今日处理简历" value={stats.processedResumeCount} suffix="份" compact={compact} mobile={mobile} />
    <Box sx={{ width: "1px", height: mobile ? 22 : compact ? 26 : 32, bgcolor: "divider" }} />
    <StatValue label="今日注册" value={stats.todayRegisteredCount} suffix="人" compact={compact} mobile={mobile} />
  </Stack>;
}

/** StatValue 展示一个统计数字及说明。 */
function StatValue({ label, value, suffix, compact, mobile }: { label: string; value: number | null; suffix: string; compact: boolean; mobile: boolean }) {
  return <Box>
    <Typography sx={{ color: "text.primary", fontWeight: 800, fontSize: mobile ? 12 : compact ? 15 : 22, lineHeight: 1.2, whiteSpace: "nowrap" }}>
      {value === null ? "--" : value.toLocaleString("zh-CN")}<Typography component="span" sx={{ ml: 0.25, color: "text.secondary", fontSize: mobile ? 8 : compact ? 11 : 13 }}>{suffix}</Typography>
    </Typography>
    <Typography sx={{ mt: compact ? 0.15 : 0.5, color: "text.secondary", fontSize: mobile ? 8 : compact ? 10 : 13, whiteSpace: "nowrap" }}>{label}</Typography>
  </Box>;
}
