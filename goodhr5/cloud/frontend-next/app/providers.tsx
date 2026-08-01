/** 本文件负责向 Next.js 页面注入 MUI 缓存和统一主题。 */
"use client";

import { AppRouterCacheProvider } from "@mui/material-nextjs/v16-appRouter";
import { CssBaseline, ThemeProvider } from "@mui/material";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { createGoodHRTheme, type MembershipTheme } from "./theme";

type ProvidersProps = { children: ReactNode };

const MembershipThemeContext = createContext<{
  membershipTheme: MembershipTheme;
  setMembershipTheme: (value: MembershipTheme) => void;
}>({ membershipTheme: "free", setMembershipTheme: () => undefined });

/** useMembershipTheme 返回当前会员主题和应用方法。 */
export function useMembershipTheme() {
  return useContext(MembershipThemeContext);
}

/** Providers 提供全局 MUI 主题和服务端样式缓存。 */
export default function Providers({ children }: ProvidersProps) {
  const [membershipTheme, setMembershipThemeState] =
    useState<MembershipTheme>("free");
  const theme = useMemo(
    () => createGoodHRTheme(membershipTheme),
    [membershipTheme],
  );

  /** setMembershipTheme 应用当前有效会员对应的后台主题。 */
  const setMembershipTheme = useCallback((value: MembershipTheme) => {
    setMembershipThemeState(value);
  }, []);

  return (
    <AppRouterCacheProvider options={{ key: "goodhr" }}>
      <MembershipThemeContext.Provider
        value={{ membershipTheme, setMembershipTheme }}
      >
        <ThemeProvider theme={theme}>
          <CssBaseline />
          {children}
        </ThemeProvider>
      </MembershipThemeContext.Provider>
    </AppRouterCacheProvider>
  );
}
