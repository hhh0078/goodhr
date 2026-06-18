/** 本文件提供新版后台页面复用的标题、空状态和信息块。 */

import { Box, Button, Stack, Typography } from "@mui/material";
import type { ReactNode } from "react";

/** PageHeader 展示后台页面标题和右侧操作。 */
export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return <Stack direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ mb: 2.5, alignItems: { sm: "center" }, justifyContent: "space-between" }}><Box sx={{ minWidth: 0 }}><Typography component="h1" sx={{ fontSize: { xs: 26, md: 30 }, fontWeight: 780 }}>{title}</Typography>{description ? <Typography sx={{ mt: 0.5, color: "text.secondary" }}>{description}</Typography> : null}</Box>{actions ? <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", rowGap: 1 }}>{actions}</Stack> : null}</Stack>;
}

/** SectionPanel 输出后台标准内容区域。 */
export function SectionPanel({ children, sx = {} }: { children: ReactNode; sx?: Record<string, unknown> }) {
  return <Box component="section" sx={{ p: { xs: 2, md: 2.5 }, borderRadius: "8px", border: "1px solid", borderColor: "divider", bgcolor: "#fbfdfc", ...sx }}>{children}</Box>;
}

/** EmptyState 展示暂无数据提示。 */
export function EmptyState({ text = "暂无数据", action }: { text?: string; action?: ReactNode }) {
  return <Stack spacing={1.5} sx={{ py: 7, alignItems: "center", color: "text.secondary" }}><Typography>{text}</Typography>{action}</Stack>;
}

/** RefreshButton 输出统一刷新按钮。 */
export function RefreshButton({ loading, onClick }: { loading?: boolean; onClick: () => void }) {
  return <Button variant="outlined" disabled={loading} onClick={onClick}>{loading ? "刷新中" : "刷新"}</Button>;
}
