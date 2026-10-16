/* 文件作用：只读当前 BOSS 附件的可见同源 PDF 文本层，在本机整理纯文本；不读取内部变量、不截图、不修改招聘页面。 */
(function (root) {
  "use strict";
  const LIMIT = 200000;

  /** visible 判断元素及父级是否显示；不要求已滚入视口，允许读取已加载的其它 PDF 页。 */
  function visible(node) {
    if (!node || node.hidden || node.closest?.("[hidden],[aria-hidden='true']")) return false;
    if (node.getClientRects && !node.getClientRects().length) return false;
    for (let item = node; item; item = item.parentElement) {
      const style = item.ownerDocument?.defaultView?.getComputedStyle?.(item);
      if (style && (style.display === "none" || ["hidden", "collapse"].includes(style.visibility) || style.opacity === "0")) return false;
    }
    return true;
  }

  /** ownText 只取当前呈现片段中的可见文字，避开嵌套呈现片段导致的重复。 */
  function ownText(node) {
    let text = "";
    for (const child of node.childNodes || []) {
      if (child.nodeType === 3) text += child.nodeValue || "";
      else if (child.nodeType === 1 && !child.matches?.("span[role='presentation']") && visible(child)) text += ownText(child);
    }
    return text.replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u200b-\u200d\ufeff]/g, "").replace(/\s+/g, " ").trim();
  }

  /** cleanPieces 只删除高度确信的重复旋转水印；普通旋转正文继续保留。 */
  function cleanPieces(pieces) {
    const counts = new Map();
    for (const piece of pieces) counts.set(piece.text, (counts.get(piece.text) || 0) + 1);
    const watermarks = new Set(pieces.filter((piece) => /^[A-Za-z0-9_-]{24,}$/.test(piece.text) && counts.get(piece.text) >= 3 && pieces.some((other) => other.text === piece.text && Math.abs(other.rotation) >= 20)).map((piece) => piece.text));
    return pieces.filter((piece) => !watermarks.has(piece.text) && !(piece.text.length === 1 && Math.abs(piece.rotation) >= 20 && [...watermarks].some((mark) => mark.includes(piece.text))));
  }

  /** rowText 按水平位置拼接同一行，给明显的词间空隙留一个空格。 */
  function rowText(pieces) {
    const ordered = [...pieces].sort((a, b) => a.x - b.x || a.order - b.order);
    return ordered.map((piece, index) => (index && piece.x - (ordered[index - 1].x + ordered[index - 1].width) > Math.max(2, piece.height * .2) ? " " : "") + piece.text).join("").trim();
  }

  /** layoutText 按视觉行排序，连续双栏区域先读左栏再读右栏，不使用 PDF 的杂乱 DOM 顺序。 */
  function layoutText(input, pageWidth) {
    const pieces = cleanPieces(input), rows = [];
    for (const piece of [...pieces].sort((a, b) => a.y - b.y || a.x - b.x || a.order - b.order)) {
      const previous = rows[rows.length - 1];
      if (previous && Math.abs(previous.y - piece.y) <= Math.max(2, Math.min(previous.height, piece.height) * .48)) previous.items.push(piece);
      else rows.push({ y: piece.y, height: piece.height || 12, items: [piece] });
    }
    const lines = [];
    for (let index = 0; index < rows.length;) {
      const band = [], split = pageWidth * .48;
      /** twoColumns 判断一行是否存在左右两块，避免把正常单行句子硬切开。 */
      const twoColumns = (row) => row.items.some((item) => item.x < split && item.x + item.width < split) && row.items.some((item) => item.x >= split && !/^[\d年月.\-—~至今/\s]+$/.test(item.text)) && row.items.every((item) => item.width < pageWidth * .48);
      if (!twoColumns(rows[index])) { lines.push(rowText(rows[index++].items)); continue; }
      let end = index;
      while (end < rows.length && twoColumns(rows[end]) && (!band.length || rows[end].y - rows[end - 1].y < Math.max(rows[end].height * 3, 40))) band.push(rows[end++]);
      if (band.length < 2) { lines.push(rowText(rows[index++].items)); continue; }
      for (const right of [false, true]) for (const row of band) lines.push(rowText(row.items.filter((item) => (item.x >= split) === right)));
      index = end;
    }
    return lines.filter(Boolean).join("\n");
  }

  /** pageText 收集单页真正的呈现片段，只保留文字和排版坐标，不返回 HTML。 */
  function pageText(page) {
    const layer = [...page.querySelectorAll(".textLayer")].find(visible);
    if (!layer) return { text: "", loaded: false };
    const pageRect = page.getBoundingClientRect(), pieces = [];
    for (const node of layer.querySelectorAll("span[role='presentation']")) {
      if (!visible(node)) continue;
      const text = ownText(node); if (!text) continue;
      const box = node.getBoundingClientRect(), style = node.getAttribute("style") || "";
      const rotation = Number((style.match(/--rotate\s*:\s*(-?[\d.]+)/) || style.match(/rotate\(\s*(-?[\d.]+)/) || [])[1]) || 0;
      pieces.push({ text, x: box.left - pageRect.left, y: box.top - pageRect.top, width: box.width, height: box.height || 12, rotation, order: pieces.length });
    }
    return { text: layoutText(pieces, pageRect.width || 747), loaded: pieces.length > 0 };
  }

  /** documents 仅递归同源且可见的 iframe；不读取跨域、隐藏预览或插件自己的浮窗。 */
  function documents(doc, depth = 0, seen = new Set()) {
    if (!doc || depth > 4 || seen.has(doc)) return [];
    seen.add(doc); const result = [doc];
    for (const frame of doc.querySelectorAll("iframe")) {
      if (!visible(frame)) continue;
      try { const child = frame.contentDocument; if (child) result.push(...documents(child, depth + 1, seen)); }
      catch { /* 跨域附件不扩大权限，不尝试绕过浏览器隔离。 */ }
    }
    return result;
  }

  /** declaredPages 只采信明确页数字段；没有总页数时交给用户核对，不把已挂载页数冒充总页数。 */
  function declaredPages(viewer, doc) {
    const candidates = [viewer, ...doc.querySelectorAll("[data-page-count], #numPages, .bzl-office-pdf-page-count")];
    for (const node of candidates) {
      const value = node.getAttribute?.("data-page-count") || node.textContent?.trim();
      if (/^\/?\s*\d{1,4}(?:\s*页)?$/.test(value || "")) { const count = Number(value.match(/\d+/)[0]); if (count > 0 && count <= 1000) return count; }
    }
    return 0;
  }

  /** extract 读取唯一可见 BOSS PDF，报告多页缺失及总页数无法确认的限制。 */
  function extract(doc, href) {
    let sourceURL;
    try { const url = new URL(href); if (url.protocol !== "https:" || url.hostname !== "www.zhipin.com" || url.port || url.username || url.password) return null; sourceURL = url.origin + url.pathname; }
    catch { return null; }
    const matches = documents(doc).flatMap((item) => [...item.querySelectorAll(".bzl-office-pdf")].filter(visible).map((viewer) => ({ viewer, doc: item })));
    if (!matches.length) return null;
    if (matches.length !== 1) return { platform_id: "boss", source_url: sourceURL, attachment: { text: "", page_count: 0, total_pages: 0, complete: false, blocked: true, warning: "看到了多份附件，请先只保留当前候选人的一份预览" } };
    const { viewer, doc: frameDoc } = matches[0], pages = [...viewer.querySelectorAll(".page[data-page-number]")].filter(visible).map((page) => ({ number: Number(page.getAttribute("data-page-number")), ...pageText(page) })).sort((a, b) => a.number - b.number);
    const total = declaredPages(viewer, frameDoc), text = pages.map((page) => page.text).filter(Boolean).join("\n\n").trim();
    const consecutive = pages.length > 0 && pages.every((page, index) => page.number === index + 1 && page.loaded);
    const complete = Boolean(total && consecutive && pages.length === total), tooLong = Array.from(text).length > LIMIT;
    const warning = tooLong ? "附件超过20万字，请换一份较短的简历" : !text ? "附件文字还没加载，或这份是图片简历；请先查看附件后重读" : total && !complete ? `只读到 ${pages.filter((page) => page.loaded).length}/${total} 页，请手动滚动载入所有页后重读` : !total ? `已读到 ${pages.length} 页文字，总页数还需你核对一下` : "";
    return { platform_id: "boss", source_url: sourceURL, attachment: { text: tooLong ? "" : text, page_count: pages.filter((page) => page.loaded).length, total_pages: total, complete, blocked: tooLong || !text || !consecutive || Boolean(total && !complete), warning } };
  }

  const api = { visible, ownText, cleanPieces, layoutText, documents, declaredPages, extract };
  root.GoodHRBossAttachment = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
