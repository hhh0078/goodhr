/** 本文件负责文章不存在、未公开或页码无效时的公开返回入口。 */

import Link from "next/link";
import MarketingShell from "@/components/MarketingShell";
import styles from "./articles.module.css";

/** ArticleNotFound 引导访客回到文章目录，不展示未发布内容。 */
export default function ArticleNotFound() {
  return <MarketingShell eyebrow="文章/动态" title="这篇文章暂时找不到了" description="可以先回目录看看，新的内容还在继续更新。"><div className="public-container"><div className={styles.notice}><Link href="/articles" className={styles.back}>返回文章目录</Link></div></div></MarketingShell>;
}
