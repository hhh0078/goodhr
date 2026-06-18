/** 本文件负责读取并展示官网公开统计数据。 */
"use client";

import { Box, Skeleton, Stack, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { apiRequest } from "@/lib/api";

type Stats = { processedResumeCount: number | null; todayRegisteredCount: number | null };

/** PublicStats 展示已处理简历和今日新注册数量，compact 控制导航紧凑样式。 */
export default function PublicStats({ compact = false }: { compact?: boolean }) {
  const [stats, setStats] = useState<Stats>({ processedResumeCount: null, todayRegisteredCount: null });

  useEffect(() => {
    let active = true;
    apiRequest("/api/public/stats/today")
      .then((data) => {
        if (!active) return;
        setStats({
          processedResumeCount: safeNumber(data.processed_resume_count),
          todayRegisteredCount: safeNumber(data.today_registered_count),
        });
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, []);

  return (
    <Stack direction="row" spacing={compact ? 1.5 : 3} sx={{ alignItems: "center", flexWrap: "wrap" }}>
      <StatValue label="已处理简历" value={stats.processedResumeCount} suffix="份" compact={compact} />
      <Box sx={{ width: "1px", height: compact ? 26 : 32, bgcolor: "divider" }} />
      <StatValue label="今日新注册" value={stats.todayRegisteredCount} suffix="人" compact={compact} />
    </Stack>
  );
}

/** StatValue 展示一个统计数字及其说明。 */
function StatValue({ label, value, suffix, compact }: { label: string; value: number | null; suffix: string; compact: boolean }) {
  return (
    <Box>
      {value === null ? (
        <Skeleton width={compact ? 48 : 72} height={compact ? 22 : 30} />
      ) : (
        <Typography sx={{ color: "text.primary", fontWeight: 800, fontSize: compact ? 15 : 22, lineHeight: 1.2, whiteSpace: "nowrap" }}>
          {value.toLocaleString("zh-CN")}
          <Typography component="span" sx={{ ml: 0.35, color: "text.secondary", fontSize: compact ? 11 : 13 }}>
            {suffix}
          </Typography>
        </Typography>
      )}
      <Typography sx={{ mt: compact ? 0.15 : 0.5, color: "text.secondary", fontSize: compact ? 10 : 13, whiteSpace: "nowrap" }}>{label}</Typography>
    </Box>
  );
}

/** safeNumber 将接口值转换成安全的非负数字。 */
function safeNumber(value: unknown) {
  const parsed = Number(value || 0);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
}
