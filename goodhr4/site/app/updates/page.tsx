import type { Metadata } from "next";
import { UpdateTimeline } from "@/components/update-timeline";
import { fetchUpdates } from "@/lib/api";

export const metadata: Metadata = {
  title: "更新记录",
  description: "GoodHR 官网更新记录，展示版本迭代、修复与优化明细。",
};

export const revalidate = 60;

export default async function UpdatesPage() {
  let updates: Awaited<ReturnType<typeof fetchUpdates>> = [];
  try {
    updates = await fetchUpdates();
  } catch {
    updates = [];
  }

  return (
    <section>
      <h1>更新记录</h1>
      <p className="lead">以下内容来自后端 update_records 表。</p>
      <UpdateTimeline updates={updates} />
    </section>
  );
}
