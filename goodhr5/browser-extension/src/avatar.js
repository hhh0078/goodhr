/* 文件作用：在插件后台按保存点击读取白名单头像，限制大小并用离页面画布规范化；不访问招聘 DOM 或 Cookie。 */
(function (root) {
  "use strict";
  const shared = root.GoodHRShared || (typeof require === "function" ? require("./shared.js") : null);
  const MAX_SOURCE_BYTES = 1024 * 1024, MAX_STORED_CHARACTERS = 128 * 1024, MAX_STORED_BYTES = Math.floor((MAX_STORED_CHARACTERS - 32) / 4) * 3;

  /** detectImageType 以图片文件头识别支持格式，拒绝伪装的 HTML 或 SVG。 */
  function detectImageType(bytes) {
    if (bytes.length >= 8 && [137,80,78,71,13,10,26,10].every((value, index) => bytes[index] === value)) return "image/png";
    if (bytes.length >= 3 && bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255) return "image/jpeg";
    if (bytes.length >= 6 && String.fromCharCode(...bytes.slice(0, 6)).match(/^GIF8[79]a$/)) return "image/gif";
    if (bytes.length >= 12 && String.fromCharCode(...bytes.slice(0, 4)) === "RIFF" && String.fromCharCode(...bytes.slice(8, 12)) === "WEBP") return "image/webp";
    return "";
  }

  /** bytesToDataURL 将小型合法图片编码为内嵌数据，避免保存第三方外链。 */
  function bytesToDataURL(bytes, type) {
    let binary = ""; for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.slice(offset, offset + 8192));
    return `data:${type};base64,${root.btoa(binary)}`;
  }

  /** readLimitedResponse 分块限制图片响应体，不允许超大头像占用后台内存。 */
  async function readLimitedResponse(response) {
    const declared = Number(response.headers?.get?.("Content-Length"));
    if (declared > MAX_SOURCE_BYTES) throw new Error("头像超过 1 MiB，可先保存简历，稍后重新读取后重试头像");
    if (!response.body?.getReader) { const bytes = new Uint8Array(await response.arrayBuffer()); if (!bytes.length || bytes.length > MAX_SOURCE_BYTES) throw new Error("头像大小不合适，可先保存简历，稍后重新读取后重试头像"); return bytes; }
    const reader = response.body.getReader(), chunks = []; let size = 0;
    try { while (true) { const chunk = await reader.read(); if (chunk.done) break; size += chunk.value.length; if (size > MAX_SOURCE_BYTES) throw new Error("头像超过 1 MiB，可先保存简历，稍后重新读取后重试头像"); chunks.push(chunk.value); } }
    finally { await reader.cancel().catch(() => {}); }
    if (!size) throw new Error("头像内容暂时没读到，请稍后重试");
    const bytes = new Uint8Array(size); let offset = 0; for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; } return bytes;
  }

  /** normalizeImage 解码后在后台 OffscreenCanvas 缩至256像素，剥元数据并限制输出128KiB。 */
  async function normalizeImage(bytes, type, environment = root, attachment = false) {
    if (!environment.createImageBitmap || !environment.OffscreenCanvas) throw new Error("当前浏览器暂不能整理头像，可更新浏览器后重试");
    const bitmap = await environment.createImageBitmap(new Blob([bytes], { type }));
    try {
      if (!bitmap.width || !bitmap.height || bitmap.width > 4096 || bitmap.height > 4096 || bitmap.width * bitmap.height > 8000000) throw new Error("头像尺寸太大，可先保存简历，稍后重新读取后重试头像");
      if (attachment && !shared.attachmentAvatarDimensions(bitmap.width, bitmap.height)) return "";
      const scale = Math.min(1, 256 / Math.max(bitmap.width, bitmap.height));
      const canvas = new environment.OffscreenCanvas(Math.max(1, Math.round(bitmap.width * scale)), Math.max(1, Math.round(bitmap.height * scale)));
      const context = canvas.getContext("2d"); if (!context) throw new Error("头像暂时没整理好，请稍后重试");
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      let blob = await canvas.convertToBlob({ type: "image/png" });
      if (blob.size > MAX_STORED_BYTES) { context.globalCompositeOperation = "destination-over"; context.fillStyle = "#ffffff"; context.fillRect(0, 0, canvas.width, canvas.height); blob = await canvas.convertToBlob({ type: "image/jpeg", quality: .8 }); }
      if (!blob.size || blob.size > MAX_STORED_BYTES) throw new Error("头像整理后仍太大，可先保存简历，稍后重新读取后重试头像");
      const normalized = new Uint8Array(await blob.arrayBuffer()), normalizedType = detectImageType(normalized);
      if (!["image/png", "image/jpeg"].includes(normalizedType)) throw new Error("头像格式暂不支持，可先保存简历，稍后重新读取后重试头像");
      const dataURL = bytesToDataURL(normalized, normalizedType); if (!shared.validStoredAvatar(dataURL)) throw new Error("头像整理后仍太大，可先保存简历，稍后重新读取后重试头像"); return dataURL;
    } finally { bitmap.close?.(); }
  }

  /** fetchAvatar 只在保存时读取白名单源，拒绝重定向且不发送网站 Cookie。 */
  async function fetchAvatar(source, pageURL, environment = root, attachment = false) {
    const url = shared.safeAvatarSource(source, pageURL); if (!url) throw new Error("头像地址不在支持范围，可先保存简历，稍后重新读取后重试头像");
    let bytes;
    if (url.startsWith("data:")) { const binary = root.atob(url.split(",")[1]); bytes = Uint8Array.from(binary, (value) => value.charCodeAt(0)); }
    else { const response = await environment.fetch(url, { credentials: "omit", redirect: "error", referrerPolicy: "no-referrer", signal: AbortSignal.timeout(8000) }); if (!response.ok) throw new Error("头像暂时读不到，可重试或稍后重新读取后重试头像"); bytes = await readLimitedResponse(response); }
    if (!bytes.length || bytes.length > MAX_SOURCE_BYTES) throw new Error("头像大小不合适，可先保存简历，稍后重新读取后重试头像");
    const type = detectImageType(bytes); if (!type) throw new Error("头像格式没认出来，可先保存简历，稍后重新读取后重试头像");
    return normalizeImage(bytes, type, environment, attachment);
  }

  /** fetchAttachmentAvatar 仅在保存时核实附件独立图片的实际尺寸，多个合理候选或读取不确定时安全留空。 */
  async function fetchAttachmentAvatar(input, pageURL, environment = root) {
    const candidates = shared.sanitizeAvatarCandidates(input, pageURL);
    if (!candidates.length || candidates.filter((item) => item.width && item.height).length > 1) return "";
    try {
      const images = (await Promise.all(candidates.map((item) => fetchAvatar(item.source_url, pageURL, environment, true)))).filter(Boolean);
      return images.length === 1 ? images[0] : "";
    } catch { return ""; /* 无法核实所有候选时不猜测，不影响简历正文保存。 */ }
  }

  const api = { MAX_SOURCE_BYTES, MAX_STORED_BYTES, MAX_STORED_CHARACTERS, detectImageType, readLimitedResponse, normalizeImage, fetchAvatar, fetchAttachmentAvatar };
  root.GoodHRAvatar = api; if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
