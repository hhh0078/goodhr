/** 文件作用：保留旧备用AI链接，统一转到公共系统配置中的备用分区。 */
import { redirect } from "next/navigation";

/** AIFallbackPage 将旧书签定位到公共配置的备用AI表单，不另建编辑入口。 */
export default function AIFallbackPage() {
  redirect("/admin/system-config?key=system.app_config&section=ai-fallback");
}
