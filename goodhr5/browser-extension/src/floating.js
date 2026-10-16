/* 文件作用：在招聘页面创建插件自己的隔离悬浮入口；只更新本容器，不改变招聘业务 DOM 或操作页面。 */
(function (root) {
  "use strict";
  const extension = root.browser || root.chrome;
  let host = null, panel = null, frame = null, currentIdentity = "", currentPhone = "";
  let moved = false;

  /** makeElement 在插件自己的 Shadow DOM 内创建静态 UI，不插入不可信 HTML。 */
  function makeElement(tag, className, text = "") { const node = document.createElement(tag); node.className = className; node.textContent = text; return node; }

  /** moveWithinWindow 只移动插件宿主，并把整个可见面板限制在浏览器窗口内。 */
  function moveWithinWindow(left, top) {
    const rect = host.getBoundingClientRect();
    host.style.setProperty("--goodhr-floating-left", `${Math.max(8, Math.min(left, root.innerWidth - rect.width - 8))}px`);
    host.style.setProperty("--goodhr-floating-top", `${Math.max(8, Math.min(top, root.innerHeight - rect.height - 8))}px`);
    host.style.setProperty("--goodhr-floating-right", "auto"); host.style.setProperty("--goodhr-floating-bottom", "auto");
  }

  /** bindDragging 仅在插件标题栏拖动自己的容器，不滚动、点击或改写招聘页面。 */
  function bindDragging(grip) {
    let dragging = null;
    grip.addEventListener("pointerdown", (event) => {
      if (event.button !== 0 || event.target.closest("button")) return;
      const rect = host.getBoundingClientRect(); dragging = { id: event.pointerId, x: event.clientX - rect.left, y: event.clientY - rect.top };
      grip.setPointerCapture(event.pointerId); event.preventDefault(); event.stopPropagation();
    });
    grip.addEventListener("pointermove", (event) => { if (!dragging || dragging.id !== event.pointerId) return; moved = true; moveWithinWindow(event.clientX - dragging.x, event.clientY - dragging.y); event.stopPropagation(); });
    /** stopDrag 结束当前插件拖拽并释放自身指针，不改变页面焦点。 */
    function stopDrag(event) { if (!dragging || dragging.id !== event.pointerId) return; dragging = null; if (grip.hasPointerCapture(event.pointerId)) grip.releasePointerCapture(event.pointerId); event.stopPropagation(); }
    grip.addEventListener("pointerup", stopDrag); grip.addEventListener("pointercancel", stopDrag);
  }

  /** setOpen 仅展开或收起插件面板；收起时保留 iframe 输入，避免备注草稿丢失。 */
  function setOpen(value) {
    panel.hidden = !value; host.hidden = !value;
    if (!value) extension.runtime.sendMessage?.({ type: "FLOATING_CLOSED" })?.catch?.(() => {});
    if (value && !frame) {
      frame = makeElement("iframe", "frame"); frame.title = "GoodHR 简历助手"; frame.referrerPolicy = "no-referrer";
      frame.src = extension.runtime.getURL("src/floating.html"); panel.appendChild(frame);
      // 不监听网页 postMessage；招聘页面无法指挥扩展登录、查重或保存。
    }
    if (value && moved) { const rect = host.getBoundingClientRect(); moveWithinWindow(rect.left, rect.top); }
  }

  /** mount 创建唯一、正常命名的隔离入口；不使用随机标识或检测规避伪装。 */
  function mount() {
    if (host?.isConnected) return;
    host = document.createElement("aside"); host.id = "goodhr-resume-assistant"; host.setAttribute("aria-label", "GoodHR 简历助手入口");
    const shadow = host.attachShadow({ mode: "closed" });
    const style = makeElement("style", ""); style.textContent = `
      :host{all:initial!important;position:fixed!important;display:block!important;left:var(--goodhr-floating-left,auto)!important;top:var(--goodhr-floating-top,auto)!important;right:var(--goodhr-floating-right,20px)!important;bottom:var(--goodhr-floating-bottom,22px)!important;z-index:2147483647!important;max-width:calc(100vw - 24px)!important;color-scheme:light!important}
      :host([hidden]){display:none!important}*{box-sizing:border-box}.shell{font:13px/1.5 system-ui,-apple-system,"Microsoft YaHei",sans-serif;color:#233e35;border:1px solid #b9ddcf;border-radius:12px;background:#fff;box-shadow:0 6px 24px #174c3621;overflow:hidden}
      .grip{cursor:grab;touch-action:none;user-select:none}
      button{font:inherit;border:1px solid #0b8968;border-radius:7px;background:#0b8968;color:#fff;padding:7px 12px;cursor:pointer;white-space:nowrap}button:hover{background:#087253}button:focus-visible{outline:2px solid #0b8968;outline-offset:2px}
      .panel{width:248px;max-width:calc(100vw - 26px)}.bar{display:flex;justify-content:space-between;align-items:center;height:26px;padding:0 8px;font-size:12px;color:#6b8b7e;background:#f4fbf7}.handle{font-size:16px;letter-spacing:2px}.close{background:transparent;color:#477a68;border-color:transparent;padding:0 6px;font-size:18px;line-height:22px}
      .frame{display:block;border:0;background:#fff;width:100%;height:min(460px,calc(100vh - 64px))}[hidden]{display:none!important}`;
    const shell = makeElement("div", "shell");
    panel = makeElement("section", "panel"); panel.hidden = true;
    const bar = makeElement("div", "bar grip"), label = makeElement("span", "handle", "⋮⋮"), close = makeElement("button", "close", "×"); close.type = "button"; close.setAttribute("aria-label", "收起 GoodHR 简历助手"); label.setAttribute("aria-hidden", "true"); bar.setAttribute("aria-label", "拖动 GoodHR 简历助手"); bindDragging(bar);
    close.addEventListener("click", () => setOpen(false)); bar.append(label, close); panel.appendChild(bar);
    shell.append(panel); shadow.append(style, shell); document.documentElement.appendChild(host);
    shadow.addEventListener("click", (event) => event.stopPropagation());
    root.addEventListener("resize", () => { if (moved && host && !host.hidden) { const rect = host.getBoundingClientRect(); moveWithinWindow(rect.left, rect.top); } });
  }

  /** identityOf 用平台身份隔离候选人，同名不同编号也会重新展开；不依赖人工输入字段。 */
  function identityOf(resume) {
    return root.GoodHRShared?.resumeIdentity(resume) || JSON.stringify([resume.platform_id || "", resume.platform_candidate_id || "", resume.source_url || "", resume.candidate_name, resume.birth_ym || "", resume.educations?.[0]?.school_name || ""]);
  }

  /** update 自动显示新候选人的简历面板，同一人主动收起后保持收起，资料消失只隐藏自己的容器。 */
  function update(resume) {
    if (!resume?.candidate_name && !resume?.attachment) { if (host && !host.hidden) setOpen(false); currentIdentity = ""; currentPhone = ""; return; }
    const identity = identityOf(resume), phone = root.GoodHRShared ? root.GoodHRShared.normalizePhone(resume.phone) : resume.phone || "";
    const changed = identity !== currentIdentity || Boolean(phone && currentPhone && phone !== currentPhone);
    mount(); host.setAttribute("aria-label", `${resume.candidate_name || "BOSS 附件"} 的 GoodHR 简历助手`);
    currentIdentity = identity; currentPhone = changed ? phone : phone || currentPhone;
    if (changed) setOpen(true);
  }

  /** close 仅在身份仍对应当前候选人时收起插件卡片，避免迟到的保存结果关闭下一人的卡片。 */
  function close(identity = "") {
    if (!host || !panel || (identity && identity !== currentIdentity)) return false;
    setOpen(false); return true;
  }

  root.GoodHRFloating = Object.freeze({ update, close });
})(globalThis);
