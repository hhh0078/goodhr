/** 本文件负责向 Next.js 页面注入 MUI 缓存和统一主题。 */
"use client";

import { AppRouterCacheProvider } from "@mui/material-nextjs/v16-appRouter";
import { CssBaseline, ThemeProvider } from "@mui/material";
import type { ReactNode } from "react";
import theme from "./theme";

type ProvidersProps = { children: ReactNode };

/** Providers 提供全局 MUI 主题和服务端样式缓存。 */
export default function Providers({ children }: ProvidersProps) {
  return (
    <AppRouterCacheProvider options={{ key: "goodhr" }}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        {children}
      </ThemeProvider>
    </AppRouterCacheProvider>
  );
}
