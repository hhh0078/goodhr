/* 文件作用：只读检测可见详情并报告资料，显示插件自己的隔离入口，不操作招聘页面业务。 */
(function () {
  "use strict";
  const extension = globalThis.browser || globalThis.chrome;
  let lastFingerprint = "", pendingTimer = null, captureSequence = 0;

  /** capture 先同步后台快照再显示自己的面板，忽略切人后的迟到响应，不提交云端。 */
  async function capture(force = false) {
    const resume = GoodHRExtraction.extractCurrentCandidate(document, location.href);
    const fingerprint = JSON.stringify(resume);
    if (!force && fingerprint === lastFingerprint) return;
    const sequence = ++captureSequence;
    lastFingerprint = fingerprint;
    try {
      await extension.runtime.sendMessage({ type: "CANDIDATE_SNAPSHOT", resume });
      if (sequence !== captureSequence) return;
      // 自然页面变化仍可能处于防抖等待中，显示前再核对一次，避免旧人短暂出现。
      if (JSON.stringify(GoodHRExtraction.extractCurrentCandidate(document, location.href)) !== fingerprint) { await capture(); return; }
      globalThis.GoodHRFloating?.update(resume);
    } catch { if (sequence === captureSequence) lastFingerprint = ""; /* 下一次自然检测可重试，不显示未同步的旧资料。 */ }
  }

  /** scheduleCapture 合并页面自然变化，避免每个节点变化都重复扫描。 */
  function scheduleCapture() { clearTimeout(pendingTimer); pendingTimer = setTimeout(() => capture(), 450); }

  /** handleRuntimeMessage 仅响应同一插件的重读与收起请求，不接受页面自定义事件。 */
  function handleRuntimeMessage(message, sender, sendResponse) {
    if (sender.id !== extension.runtime.id) return false;
    if (message?.type === "CLOSE_FLOATING") { sendResponse({ ok: true, closed: Boolean(globalThis.GoodHRFloating?.close(message.identity || "")) }); return false; }
    if (message?.type !== "REQUEST_CAPTURE") return false;
    capture(true).then(() => sendResponse({ ok: true }), () => sendResponse({ ok: false })); return true;
  }

  extension.runtime.onMessage.addListener(handleRuntimeMessage);
  new MutationObserver(scheduleCapture).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ["class", "style", "hidden", "aria-hidden"] });
  setInterval(scheduleCapture, 2000);
  capture();
})();
