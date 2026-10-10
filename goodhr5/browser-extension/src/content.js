/* 文件作用：只读检测白名单招聘页面的可见详情，并通知插件后台；绝不修改页面或发起网络请求。 */
(function () {
  "use strict";
  const extension = globalThis.browser || globalThis.chrome;
  let lastFingerprint = "", pendingTimer = null;

  /** capture 仅把变化后的可见资料交给插件后台，不提交云端。 */
  async function capture(force = false) {
    const resume = GoodHRExtraction.extractCurrentCandidate(document, location.href);
    const fingerprint = JSON.stringify(resume);
    if (!force && fingerprint === lastFingerprint) return;
    lastFingerprint = fingerprint;
    try { await extension.runtime.sendMessage({ type: "CANDIDATE_SNAPSHOT", resume }); } catch { /* 扩展更新或禁用后停止发送即可。 */ }
  }

  /** scheduleCapture 合并页面自然变化，避免每个节点变化都重复扫描。 */
  function scheduleCapture() { clearTimeout(pendingTimer); pendingTimer = setTimeout(() => capture(), 450); }

  /** handleCaptureRequest 仅响应同一插件的重新读取请求，不接受页面自定义事件。 */
  function handleCaptureRequest(message, sender, sendResponse) {
    if (sender.id !== extension.runtime.id || message?.type !== "REQUEST_CAPTURE") return false;
    capture(true).then(() => sendResponse({ ok: true }), () => sendResponse({ ok: false })); return true;
  }

  extension.runtime.onMessage.addListener(handleCaptureRequest);
  new MutationObserver(scheduleCapture).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ["class", "style", "hidden", "aria-hidden"] });
  setInterval(scheduleCapture, 2000);
  capture();
})();
