/** 本文件负责公开文章接口短暂不可用时的重试入口，不将故障伪装为空文章目录。 */
"use client";

import Link from "next/link";
import { Button } from "antd";

/** ArticleError 提供重新读取和返回首页两个明确选择。 */
export default function ArticleError({ reset }: { reset: () => void }) {
  return <main style={{ maxWidth: 840, margin: "64px auto", padding: "0 24px", lineHeight: 1.8 }}><h1>文章还没读出来</h1><p>可以再试一次，或先回首页逛逛。</p><Button type="primary" onClick={reset}>重新读取</Button> <Link href="/">返回首页</Link></main>;
}
