/* 文件作用：在已授权的 BOSS 附件保存中固定只读核对当前 Vue 身份与可见附件，仅返回后台头像线索；不取 Cookie、不请求网络、不修改页面。 */
(function (root) {
  "use strict";

  /** probeCurrentBossAvatar 在 MAIN 世界独立运行，只读取已验证组件的有限字段；有歧义或身份不符时返回空。 */
  function probeCurrentBossAvatar() {
    "use strict";
    /** visible 逐级核对实际呈现状态，排除隐藏组件和尚未展开的旧候选人。 */
    function visible(node) {
      if (!node || node.hidden || !Array.from(node.getClientRects()).some((rect) => rect.width > 0 && rect.height > 0)) return false;
      for (let item = node; item; item = item.parentElement) {
        const style = getComputedStyle(item);
        if (item.hidden || item.getAttribute?.("aria-hidden") === "true" || style.display === "none" || ["hidden", "collapse"].includes(style.visibility) || style.opacity === "0") return false;
      }
      return true;
    }
    /** uniqueVisible 只接受一个当前呈现组件，不从多个候选人或多个附件中猜测。 */
    function uniqueVisible(selector) {
      const nodes = Array.from(document.querySelectorAll(selector)).filter(visible);
      return nodes.length === 1 ? nodes[0] : null;
    }
    /** identity 保留有限原始身份值，拒绝对象转换、空值和控制字符。 */
    function identity(value) {
      if (typeof value !== "string" && !(typeof value === "number" && Number.isSafeInteger(value))) return "";
      const text = String(value);
      return text && text.length <= 256 && text.trim() === text && !/[\u0000-\u001f\u007f]/.test(text) ? text : "";
    }
    /** bossURL 只解析当前已验证站点的 HTTPS 地址，不接受相似域名或凭证。 */
    function bossURL(value) {
      const url = new URL(value, location.href);
      return url.protocol === "https:" && url.hostname === "www.zhipin.com" && !url.port && !url.username && !url.password ? url : null;
    }
    try {
      if (!bossURL(location.href)) return null;
      const conversation = uniqueVisible(".conversation-message"), component = uniqueVisible(".resume-content");
      if (!conversation || !component) return null;
      const geek = conversation.__vue__?.geek, resume = component.__vue__?.resume, detail = component.__vue__?.detail;
      const candidateID = identity(geek?.encryptUid), userID = identity(geek?.uid);
      if (!candidateID || !userID || identity(resume?.encryptUid) !== candidateID || identity(resume?.uid) !== userID || identity(detail?.encryptGeekId) !== candidateID) return null;
      const frames = Array.from(document.querySelectorAll("iframe[src]")).filter(visible).filter((frame) => {
        const outer = bossURL(frame.getAttribute("src"));
        return outer?.pathname === "/bzl-office/pdf-viewer-b";
      });
      if (frames.length !== 1) return null;
      const outer = bossURL(frames[0].getAttribute("src"));
      if (outer.searchParams.getAll("url").length !== 1) return null;
      const inner = bossURL(outer.searchParams.get("url"));
      if (!inner || inner.searchParams.getAll("geekId").length !== 1 || inner.searchParams.get("geekId") !== candidateID || inner.pathname !== "/wflow/zpgeek/download/preview4boss/" + encodeURIComponent(candidateID)) return null;
      const source = geek?.avatar;
      if (typeof source !== "string" || !source || source.length > 4096) return null;
      const image = new URL(source);
      if (image.protocol !== "https:" || image.hostname !== "img.bosszhipin.com" || image.port || image.username || image.password || !image.pathname.startsWith("/beijin/upload/avatar/") || image.pathname.length <= "/beijin/upload/avatar/".length) return null;
      return { candidate_id: candidateID, avatar_url: image.href };
    } catch { return null; }
  }

  const api = { probeCurrentBossAvatar };
  root.GoodHRBossVueAvatar = api; if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
