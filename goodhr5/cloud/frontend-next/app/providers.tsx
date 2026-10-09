/** 本文件负责提供 Ant Design 中文配置、消息上下文和会员主题。 */
"use client";

import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  createGoodHRTheme,
  createGoodHRVariables,
  type MembershipTheme,
} from "./theme";

type ProvidersProps = { children: ReactNode };

const MembershipThemeContext = createContext<{
  membershipTheme: MembershipTheme;
  setMembershipTheme: (value: MembershipTheme) => void;
}>({ membershipTheme: "free", setMembershipTheme: () => undefined });

/** useMembershipTheme 返回当前会员主题和应用方法。 */
export function useMembershipTheme() {
  return useContext(MembershipThemeContext);
}

/** Providers 提供全局 Ant Design 主题，浮层与正文共用会员颜色。 */
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
    <ConfigProvider
      locale={zhCN}
      theme={theme}
      button={{ autoInsertSpace: false }}
    >
      <MembershipThemeContext.Provider
        value={{ membershipTheme, setMembershipTheme }}
      >
        <style>{createGoodHRVariables(membershipTheme)}</style>
        <App>{children}</App>
      </MembershipThemeContext.Provider>
    </ConfigProvider>
  );
}
