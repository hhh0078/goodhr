/** 本文件负责排障分享页的元信息，避免公开链接进入搜索结果。 */
import type { Metadata } from "next";
import DiagnosticShareView from "./share-view";

export const metadata: Metadata = {
  title: "岗位排障日志",
  robots: { index: false, follow: false },
  referrer: "no-referrer",
};

/** DiagnosticSharePage 显示创建链接时固定的岗位日志快照。 */
export default function DiagnosticSharePage() {
  return <DiagnosticShareView />;
}
