/** 本文件负责安全输出搜索引擎和 AI 检索系统可读取的 JSON-LD。 */

/** StructuredData 将结构化数据输出为 JSON-LD 脚本。 */
export default function StructuredData({ data }: { data: Record<string, unknown> | Array<Record<string, unknown>> }) {
  const json = JSON.stringify(data).replace(/</g, "\\u003c");
  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: json }} />;
}
