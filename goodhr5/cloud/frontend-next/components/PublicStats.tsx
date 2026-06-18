/** 本文件负责读取并展示官网公开统计数据。 */
"use client";

import { Box, Skeleton, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { apiRequest } from "@/lib/api";

type Stats = { processedResumeCount: number | null; todayRegisteredCount: number | null };

/** PublicStats 展示已处理简历和今日新注册数量。 */
export default function PublicStats() {
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
    <Box sx={{ display: "flex", alignItems: "center", gap: { xs: 2, md: 3 }, flexWrap: "wrap" }}>
      <StatValue label="已处理简历" value={stats.processedResumeCount} suffix="份" />
      <Box sx={{ width: "1px", height: 32, bgcolor: "divider", display: { xs: "none", sm: "block" } }} />
      <StatValue label="今日新注册" value={stats.todayRegisteredCount} suffix="人" />
    </Box>
  );
}

/** StatValue 展示一个统计数字及其说明。 */
function StatValue({ label, value, suffix }: { label: string; value: number | null; suffix: string }) {
  return (
    <Box>
      {value === null ? (
        <Skeleton width={72} height={30} />
      ) : (
        <Typography sx={{ color: "text.primary", fontWeight: 800, fontSize: 22, lineHeight: 1.2 }}>
          {value.toLocaleString("zh-CN")}
          <Typography component="span" sx={{ ml: 0.5, color: "text.secondary", fontSize: 13 }}>
            {suffix}
          </Typography>
        </Typography>
      )}
      <Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>{label}</Typography>
    </Box>
  );
}

/** safeNumber 将接口值转换成安全的非负数字。 */
function safeNumber(value: unknown) {
  const parsed = Number(value || 0);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
}
