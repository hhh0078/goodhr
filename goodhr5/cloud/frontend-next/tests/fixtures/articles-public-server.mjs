/** 本文件提供仅监听本机的合成文章 API，验证 SSR、动态站点地图与公开阅读，不使用真实业务数据。 */
import http from "node:http";

const port = Number(process.env.ARTICLE_FIXTURE_PORT || 18167);
const content = '第一段：每天少做一点重复工作。\n\n第二段：换行和空行都会保留。\n<script>window.__articleInjected=true</script>\n最后一行 & <b>这是纯文本，不是粗体标签</b>。';
const longTitle = "GoodHR 招聘实用分享：" + "候选人筛选与团队协作".repeat(8);
let mode = "normal";

/** articleID 返回可稳定对应测试数据的 UUID。 */
function articleID(index) { return `10000000-0000-4000-8000-${String(index).padStart(12, "0")}`; }
/** summary 生成公开目录只需要的轻量字段。 */
function summary(index) { return { id: articleID(index), title: index === 1 ? longTitle : `招聘文章 ${index}`, excerpt: `合成文章 ${index} 的招聘实践与产品更新。`, published_at: "2026-10-17T02:30:00Z", updated_at: "2026-10-17T03:00:00Z" }; }
/** json 返回不缓存的合成 JSON 响应。 */
function json(response, status, value) { response.writeHead(status, { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" }); response.end(JSON.stringify(value)); }

/** handleRequest 提供公开数据与本机测试状态切换，错误状态用于验证 sitemap 不误删链接。 */
function handleRequest(request, response) {
  const url = new URL(request.url, `http://127.0.0.1:${port}`);
  if (url.pathname === "/__test__/articles") { mode = url.searchParams.get("mode") || "normal"; return json(response, 200, { ok: true, mode }); }
  if (url.pathname === "/api/public/stats/today") return json(response, 200, { processed_resume_count: 1234, today_registered_count: 12 });
  if (!url.pathname.startsWith("/api/public/articles")) return json(response, 404, { error: "fixture endpoint not found" });
  if (mode === "unavailable") return json(response, 503, { error: "synthetic unavailable" });
  const catalog = url.pathname === "/api/public/articles/catalog";
  if (catalog || url.pathname === "/api/public/articles") {
    const page = Number(url.searchParams.get("page") || 1);
    const pageSize = Number(url.searchParams.get("page_size") || (catalog ? 100 : 20));
    const total = mode === "empty" ? 0 : mode === "many" ? 51001 : mode === "thousand" ? 1000 : mode === "overflow" ? 1001 : mode === "deleted" ? 20 : 21;
    const start = (page - 1) * pageSize;
    const items = Array.from({ length: Math.max(0, Math.min(pageSize, total - start)) }, (_, index) => summary(start + index + (mode === "deleted" ? 2 : 1)));
    if (mode === "partial") items.pop();
    return json(response, 200, { ok: true, items, total, page, page_size: pageSize });
  }
  const id = url.pathname.split("/").at(-1);
  const index = Number(id?.split("-").at(-1));
  if (!index || index > 21 || mode === "empty" || (mode === "deleted" && index === 1)) return json(response, 404, { error: "article not found" });
  return json(response, 200, { ok: true, article: { ...summary(index), content, is_published: true } });
}

http.createServer(handleRequest).listen(port, "127.0.0.1", () => process.stdout.write(`Articles fixture ready at http://127.0.0.1:${port}\n`));
