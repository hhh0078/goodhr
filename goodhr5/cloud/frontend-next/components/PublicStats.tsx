/** 本文件负责展示服务端准备好的官网公开统计数据。 */

import { Box, Stack, Typography } from "@mui/material";
import type { PublicStatsData } from "@/lib/public-data";

/** PublicStats 展示已处理简历和今日新注册数量。 */
export default function PublicStats({ stats, compact = false }: { stats: PublicStatsData; compact?: boolean }) {
  return <Stack direction="row" spacing={compact ? 1.5 : 3} sx={{ alignItems: "center" }}>
    <StatValue label="已处理简历" value={stats.processedResumeCount} suffix="份" compact={compact} />
    <Box sx={{ width: "1px", height: compact ? 26 : 32, bgcolor: "divider" }} />
    <StatValue label="今日新注册" value={stats.todayRegisteredCount} suffix="人" compact={compact} />
  </Stack>;
}

/** StatValue 展示一个统计数字及说明。 */
function StatValue({ label, value, suffix, compact }: { label: string; value: number | null; suffix: string; compact: boolean }) {
  return <Box>
    <Typography sx={{ color: "text.primary", fontWeight: 800, fontSize: compact ? 15 : 22, lineHeight: 1.2, whiteSpace: "nowrap" }}>
      {value === null ? "--" : value.toLocaleString("zh-CN")}<Typography component="span" sx={{ ml: 0.35, color: "text.secondary", fontSize: compact ? 11 : 13 }}>{suffix}</Typography>
    </Typography>
    <Typography sx={{ mt: compact ? 0.15 : 0.5, color: "text.secondary", fontSize: compact ? 10 : 13, whiteSpace: "nowrap" }}>{label}</Typography>
  </Box>;
}
