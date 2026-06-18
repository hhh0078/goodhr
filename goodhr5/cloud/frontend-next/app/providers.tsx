/** 本文件负责向 Next.js 页面注入 MUI 缓存和统一主题。 */
"use client";

import { AppRouterCacheProvider } from "@mui/material-nextjs/v16-appRouter";
import { CssBaseline, ThemeProvider } from "@mui/material";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { createGoodHRTheme, type ThemePreference } from "./theme";

type ProvidersProps = { children: ReactNode };

const THEME_CACHE_KEY = "goodhr5_next_theme";
const ThemePreferenceContext = createContext<{ preference: ThemePreference; setPreference: (value: ThemePreference) => void }>({ preference: "green", setPreference: () => undefined });

/** useThemePreference 返回当前主题和切换方法。 */
export function useThemePreference() {
  return useContext(ThemePreferenceContext);
}

/** Providers 提供全局 MUI 主题和服务端样式缓存。 */
export default function Providers({ children }: ProvidersProps) {
  const [preference, setPreferenceState] = useState<ThemePreference>("green");
  useEffect(() => { const cached = localStorage.getItem(THEME_CACHE_KEY); if (cached === "green" || cached === "rose" || cached === "amber") setPreferenceState(cached); }, []);
  const theme = useMemo(() => createGoodHRTheme(preference), [preference]);

  /** setPreference 保存并实时应用用户选择的主题。 */
  function setPreference(value: ThemePreference) {
    setPreferenceState(value);
    localStorage.setItem(THEME_CACHE_KEY, value);
  }
  return (
    <AppRouterCacheProvider options={{ key: "goodhr" }}>
      <ThemePreferenceContext.Provider value={{ preference, setPreference }}><ThemeProvider theme={theme}><CssBaseline />{children}</ThemeProvider></ThemePreferenceContext.Provider>
    </AppRouterCacheProvider>
  );
}
