/** 本文件负责新版后台路由的统一布局和禁止索引配置。 */

import type { Metadata } from "next";
import type { ReactNode } from "react";
import AdminApp from "@/components/admin/AdminApp";

export const metadata: Metadata = { title: "控制台", robots: { index: false, follow: false } };

/** AdminLayout 为全部后台页面挂载统一应用框架。 */
export default function AdminLayout({ children }: { children: ReactNode }) {
  return <AdminApp>{children}</AdminApp>;
}
