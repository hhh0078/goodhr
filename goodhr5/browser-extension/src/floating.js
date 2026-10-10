/* 文件作用：在招聘页面创建插件自己的隔离悬浮入口；只更新本容器，不改变招聘业务 DOM 或操作页面。 */
(function (root) {
  "use strict";
  const extension = root.browser || root.chrome;
  let host = null, button = null, subtitle = null, panel = null, frame = null, opened = false, currentName = "";
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
    opened = value; panel.hidden = !value; button.setAttribute("aria-expanded", String(value));
    if (value && !frame) {
      frame = makeElement("iframe", "frame"); frame.title = "GoodHR 简历助手"; frame.referrerPolicy = "no-referrer";
      frame.src = extension.runtime.getURL("src/ui.html?surface=floating"); panel.appendChild(frame);
      // 不监听网页 postMessage；招聘页面无法指挥扩展登录、查重或保存。
    }
    if (moved) { const rect = host.getBoundingClientRect(); moveWithinWindow(rect.left, rect.top); }
  }

  /** mount 创建唯一、正常命名的隔离入口；不使用随机标识或检测规避伪装。 */
  function mount() {
    if (host?.isConnected) return;
    host = document.createElement("aside"); host.id = "goodhr-resume-assistant"; host.setAttribute("aria-label", "GoodHR 简历助手入口");
    const shadow = host.attachShadow({ mode: "closed" });
    const style = makeElement("style", ""); style.textContent = `
      :host{all:initial!important;position:fixed!important;display:block!important;left:var(--goodhr-floating-left,auto)!important;top:var(--goodhr-floating-top,auto)!important;right:var(--goodhr-floating-right,20px)!important;bottom:var(--goodhr-floating-bottom,22px)!important;z-index:2147483647!important;max-width:calc(100vw - 24px)!important;color-scheme:light!important}
      :host([hidden]){display:none!important}*{box-sizing:border-box}.shell{font:13px/1.5 system-ui,-apple-system,"Microsoft YaHei",sans-serif;color:#29271f;border:1px solid #d9d1bc;border-radius:12px;background:#fffef9;box-shadow:0 6px 24px #473e292b;overflow:hidden}
      .entry{display:flex;align-items:center;gap:10px;padding:10px 12px}.details{max-width:185px}.title{font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.subtitle{font-size:11px;color:#827a68;overflow-wrap:anywhere}
      .grip{cursor:grab;touch-action:none;user-select:none}
      button{font:inherit;border:1px solid #8a6721;border-radius:7px;background:#8a6721;color:#fff;padding:7px 10px;cursor:pointer;white-space:nowrap}button:hover{background:#725419}button:focus-visible{outline:2px solid #bea35b;outline-offset:2px}
      .panel{width:420px;max-width:calc(100vw - 26px);border-top:1px solid #e5dfd0}.bar{display:flex;justify-content:space-between;align-items:center;padding:5px 10px;font-size:12px;color:#77602c}.close{background:transparent;color:#665535;border-color:transparent;padding:3px 8px}
      .frame{display:block;border:0;background:#faf9f5;width:100%;height:min(620px,calc(100vh - 190px))}.fallback{font-size:11px;color:#827a68;margin:0;padding:4px 10px 7px}[hidden]{display:none!important}
      @media(max-width:440px){.details{max-width:145px}.entry{gap:8px;padding:9px}.frame{height:calc(100vh - 180px)}}`;
    const shell = makeElement("div", "shell"), entry = makeElement("div", "entry"), details = makeElement("div", "details grip"); bindDragging(details);
    const title = makeElement("div", "title", "添加到 GoodHR"); subtitle = makeElement("div", "subtitle");
    details.append(title, subtitle); button = makeElement("button", "open", "打开助手"); button.type = "button"; button.setAttribute("aria-expanded", "false");
    button.addEventListener("click", () => setOpen(!opened)); entry.append(details, button);
    panel = makeElement("section", "panel"); panel.hidden = true;
    const bar = makeElement("div", "bar grip"), label = makeElement("span", "", "GoodHR 简历助手"), close = makeElement("button", "close", "收起"); close.type = "button"; bindDragging(bar);
    close.addEventListener("click", () => setOpen(false)); bar.append(label, close); panel.appendChild(bar);
    panel.appendChild(makeElement("p", "fallback", "这里打不开时，可点工具栏的 GoodHR 图标继续。资料不会自动入库。"));
    shell.append(entry, panel); shadow.append(style, shell); document.documentElement.appendChild(host);
    shadow.addEventListener("click", (event) => event.stopPropagation());
    root.addEventListener("resize", () => { if (moved && host && !host.hidden) { const rect = host.getBoundingClientRect(); moveWithinWindow(rect.left, rect.top); } });
  }

  /** update 只根据当前可见候选人显示入口，资料消失则隐藏本容器，不删除招聘页面元素。 */
  function update(resume) {
    if (!resume?.candidate_name) { if (host && !host.hidden) host.hidden = true; return; }
    mount(); if (host.hidden) host.hidden = false;
    subtitle.textContent = `${resume.candidate_name} · ${resume.phone ? "已读取联系方式" : "手机号待补齐"}`;
    if (currentName !== resume.candidate_name) { currentName = resume.candidate_name; button.setAttribute("aria-label", `打开 ${currentName} 的 GoodHR 助手`); }
  }

  root.GoodHRFloating = Object.freeze({ update });
})(globalThis);
