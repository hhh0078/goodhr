/* 文件作用：只读当前 BOSS 附件的可见同源 PDF 文本层和独立图片，在本机整理纯文本及头像线索；不读内部变量、不截图、不修改招聘页面。 */
(function (root) {
  "use strict";
  const LIMIT = 200000;
  const shared = root.GoodHRShared || (typeof require === "function" ? require("./shared.js") : null);

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
    return text.normalize("NFKC").replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u200b-\u200d\ufeff]/g, "").replace(/\s+/g, " ").trim();
  }

  /** cleanPieces 只删除高度确信的重复旋转水印；普通旋转正文继续保留。 */
  function cleanPieces(pieces) {
    const counts = new Map(), rotated = new Set();
    for (const piece of pieces) { counts.set(piece.text, (counts.get(piece.text) || 0) + 1); if (Math.abs(piece.rotation) >= 20) rotated.add(piece.text); }
    const watermarks = new Set(pieces.filter((piece) => /^[A-Za-z0-9_-]{24,}[~=]{0,4}$/.test(piece.text) && counts.get(piece.text) >= 3 && rotated.has(piece.text)).map((piece) => piece.text));
    return pieces.filter((piece) => !watermarks.has(piece.text) && !(piece.text.length === 1 && Math.abs(piece.rotation) >= 20 && [...watermarks].some((mark) => mark.includes(piece.text))));
  }

  /** rowText 按水平位置拼接同一行，给明显的词间空隙留一个空格。 */
  function rowText(pieces) {
    const ordered = [...pieces].sort((a, b) => a.x - b.x || a.order - b.order);
    return ordered.map((piece, index) => { const previous = ordered[index - 1], gap = previous ? piece.x - (previous.x + previous.width) : 0, cjk = previous && /[\u2e80-\u9fff\uf900-\ufaff]$/.test(previous.text) && /^[\u2e80-\u9fff\uf900-\ufaff]/.test(piece.text); return (index && gap > Math.max(2, piece.height * (cjk ? .85 : .2)) ? " " : "") + piece.text; }).join("").trim();
  }

  /** visualRuns 先合并相邻文字片段；只有超过字号和页宽容差的真实大空隙才可能成为栏间空白。 */
  function visualRuns(row, pageWidth) {
    const ordered = [...row.items].sort((a, b) => a.x - b.x || a.order - b.order), heights = ordered.map((item) => item.height || 12).sort((a, b) => a - b), gap = Math.max(20, pageWidth * .035, (heights[Math.floor(heights.length / 2)] || 12) * 1.6), runs = [];
    for (const item of ordered) { const previous = runs[runs.length - 1], right = item.x + item.width; if (previous && item.x - previous.right < gap) { previous.right = Math.max(previous.right, right); previous.items.push(item); } else runs.push({ x: item.x, right, items: [item] }); }
    return { runs, gap };
  }

  /** columnBand 仅以多行共享的真实空白确定栏界，整段文字不能跨栏拆分；公司和日期等表格行保持横向关联。 */
  function columnBand(rows, start, pageWidth) {
    const dated = (row) => row.items.some((item) => Math.abs(item.rotation) >= 20) || /(?:^|[^\d])(?:19|20)\d{2}(?:[^\d]|$)/.test(rowText(row.items)), first = visualRuns(rows[start], pageWidth);
    if (dated(rows[start]) || first.runs.length < 2) return null;
    let best = null;
    for (let boundary = 1; boundary < first.runs.length; boundary++) {
      let left = first.runs[boundary - 1].right, right = first.runs[boundary].x, evidence = 1;
      for (let index = start + 1; index < rows.length; index++) {
        const row = rows[index]; if (dated(row) || row.y - rows[index - 1].y > Math.max(40, row.height * 3)) break;
        const { runs, gap } = visualRuns(row, pageWidth), blanks = [[-Infinity, runs[0].x], ...runs.slice(1).map((run, position) => [runs[position].right, run.x]), [runs[runs.length - 1].right, Infinity]];
        const possible = blanks.map(([low, high]) => [Math.max(left, low), Math.min(right, high)]).filter(([low, high]) => high - low >= Math.max(first.gap, gap)).sort((a, b) => (b[1] - b[0]) - (a[1] - a[0]));
        if (!possible.length) break;
        [left, right] = possible[0]; if (runs.some((run) => run.right <= left) && runs.some((run) => run.x >= right)) evidence++;
        if (evidence >= 2 && (!best || evidence > best.evidence || (evidence === best.evidence && index + 1 > best.end) || (evidence === best.evidence && index + 1 === best.end && right - left > best.width))) best = { end: index + 1, split: (left + right) / 2, evidence, width: right - left };
      }
    }
    return best;
  }

  /** layoutText 按视觉行排序，仅在多行空白已证实时按实际栏界读取；普通整行和逐字内容不按固定页中线切断。 */
  function layoutText(input, pageWidth) {
    const pieces = cleanPieces(input), rows = [];
    for (const piece of [...pieces].sort((a, b) => a.y - b.y || a.x - b.x || a.order - b.order)) {
      const previous = rows[rows.length - 1];
      const height = piece.height || 12, commonHeight = previous ? Math.min(previous.height, height) : height, overlap = previous ? Math.min(previous.y + previous.height, piece.y + height) - Math.max(previous.y, piece.y) : 0;
      if (previous && !previous.items.some((item) => Math.abs(item.rotation) >= 20) && Math.abs(piece.rotation) < 20 && (Math.abs(previous.y - piece.y) <= Math.max(2, commonHeight * .48) || (overlap >= commonHeight * .6 && Math.abs(previous.y + previous.height - piece.y - height) <= Math.max(2, commonHeight * .35)))) { previous.items.push(piece); previous.height = Math.max(previous.height, piece.y + height - previous.y); }
      else rows.push({ y: piece.y, height: piece.height || 12, items: [piece] });
    }
    const lines = [];
    for (let index = 0; index < rows.length;) {
      const band = columnBand(rows, index, pageWidth);
      if (!band) { lines.push(rowText(rows[index++].items)); continue; }
      for (const right of [false, true]) for (const row of rows.slice(index, band.end)) lines.push(rowText(row.items.filter((item) => (item.x >= band.split) === right)));
      index = band.end;
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

  /** declaredPages 只采信明确页数字段；没有总页数时仍保留未知状态，不把已挂载页数冒充总页数。 */
  function declaredPages(viewer, doc) {
    const candidates = [viewer, ...doc.querySelectorAll("[data-page-count], #numPages, .bzl-office-pdf-page-count")];
    for (const node of candidates) {
      const value = node.getAttribute?.("data-page-count") || node.textContent?.trim();
      if (/^\/?\s*\d{1,4}(?:\s*页)?$/.test(value || "")) { const count = Number(value.match(/\d+/)[0]); if (count > 0 && count <= 1000) return count; }
    }
    return 0;
  }

  /** avatarCandidates 只读取当前附件容器里的独立可见图片，跳过明确装饰和整页预览，不读取页面画布。 */
  function avatarCandidates(viewer, pageURL) {
    if (!shared) return [];
    const candidates = [];
    for (const node of viewer.querySelectorAll("img, image")) {
      if (!visible(node)) continue;
      const baseURL = shared.platformForURL(node.baseURI) === "boss" ? node.baseURI : pageURL;
      const source_url = shared.safeAvatarSource(node.currentSrc || node.getAttribute("src") || node.getAttribute("href") || node.getAttribute("xlink:href"), baseURL);
      if (!source_url) continue;
      const metadata = [node.getAttribute("alt"), node.getAttribute("title"), node.getAttribute("class"), node.getAttribute("id"), node.parentElement?.getAttribute("class"), node.parentElement?.getAttribute("id"), source_url.startsWith("data:") ? "" : new URL(source_url).pathname].filter(Boolean).join(" ");
      if (/logo|icon|qr[-_ ]?code|watermark|banner|二维码|水印|徽标|图标|横幅|标志/i.test(metadata)) continue;
      const page = node.closest?.(".page[data-page-number]"), box = node.getBoundingClientRect?.(), pageBox = page?.getBoundingClientRect?.();
      if (box && pageBox?.width && pageBox?.height && box.width >= pageBox.width * .65 && box.height >= pageBox.height * .65) continue;
      candidates.push({ source_url, width: Number(node.naturalWidth) || 0, height: Number(node.naturalHeight) || 0 });
      if (candidates.length > 6) return [];
    }
    return shared.sanitizeAvatarCandidates(candidates, pageURL);
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
    const warning = tooLong ? "附件太长，请换一份较短的简历" : !text ? "暂时读不到文字，请等附件加载；图片简历暂不支持" : total && !complete ? `正在加载附件（${pages.filter((page) => page.loaded).length}/${total} 页）` : !consecutive ? "附件还没加载完，请向下翻页" : "";
    return { platform_id: "boss", source_url: sourceURL, avatar_candidates: avatarCandidates(viewer, href), attachment: { text: tooLong ? "" : text, page_count: pages.filter((page) => page.loaded).length, total_pages: total, complete, blocked: tooLong || !text || !consecutive || Boolean(total && !complete), warning } };
  }

  const api = { visible, ownText, cleanPieces, layoutText, documents, declaredPages, avatarCandidates, extract };
  root.GoodHRBossAttachment = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
