/**
 * common.js — 注入侧原子操作脚本
 *
 * 注入到招聘网站页面，作为扩展侧的"手和眼"。
 * 只做执行不做决策，所有业务逻辑在扩展侧完成。
 *
 * 三个原子能力：
 * 1. find   — DOM 查找，带重试
 * 2. click  — DOM 点击，带等待
 * 3. send   — 与扩展侧通信（日志、状态上报）
 *
 * 消息协议（扩展侧 → common.js）：
 *   { action: "find",  selector: "css选择器", all: bool, retries, interval }
 *   { action: "click", selector: "css选择器", index, retries, interval }
 *   { action: "scroll", scrollY }
 *   { action: "mark",  selector: "css选择器", index, reason, type }
 *   { action: "ping" }
 *
 * common.js → 扩展侧：
 *   { type: "LOG_MESSAGE", data: { message, type } }
 */

(function () {
  "use strict";

  // ════════════════════════════════════════════════
  // 1. send — 与扩展侧通信
  // ════════════════════════════════════════════════

  /**
   * 向扩展侧发送消息
   * @param {object} data - 消息内容
   */
  function send(data) {
    try {
      chrome.runtime.sendMessage(data, function () {
        if (chrome.runtime.lastError) {
          console.warn(
            "[GoodHR] 消息发送失败:",
            chrome.runtime.lastError.message,
          );
        }
      });
    } catch (e) {
      console.warn("[GoodHR] 消息发送异常:", e.message);
    }
  }

  /**
   * 发送日志消息到扩展侧
   * @param {string} message - 日志内容
   * @param {string} type - 日志类型 info/success/error/warning
   */
  function sendLog(message, type) {
    send({
      type: "LOG_MESSAGE",
      data: { message: message, type: type || "info" },
    });
  }

  // ════════════════════════════════════════════════
  // 2. find — DOM 查找（带重试）
  // ════════════════════════════════════════════════

  /**
   * 查找单个元素（带重试）
   * @param {string} selector - CSS 选择器
   * @param {number} retries - 重试次数，默认 5
   * @param {number} interval - 重试间隔(ms)，默认 1000
   * @returns {Promise<object|null>} 元素信息 { text, innerHTML, rect }
   */
  function findElement(selector, retries, interval) {
    retries = retries != null ? retries : 5;
    interval = interval != null ? interval : 1000;

    return new Promise(function (resolve) {
      function attempt(remaining) {
        var el = document.querySelector(selector);
        if (el) {
          resolve(serializeElement(el, 0));
          return;
        }
        if (remaining <= 0) {
          resolve(null);
          return;
        }
        setTimeout(function () {
          attempt(remaining - 1);
        }, interval);
      }
      attempt(retries);
    });
  }

  /**
   * 查找多个元素（带重试）
   * @param {string} selector - CSS 选择器
   * @param {number} retries - 重试次数，默认 5
   * @param {number} interval - 重试间隔(ms)，默认 1000
   * @returns {Promise<object[]>} 元素信息数组
   */
  function findElementAll(selector, retries, interval) {
    retries = retries != null ? retries : 5;
    interval = interval != null ? interval : 1000;

    return new Promise(function (resolve) {
      function attempt(remaining) {
        var els = document.querySelectorAll(selector);
        if (els && els.length > 0) {
          var results = [];
          for (var i = 0; i < els.length; i++) {
            results.push(serializeElement(els[i], i));
          }
          resolve(results);
          return;
        }
        if (remaining <= 0) {
          resolve([]);
          return;
        }
        setTimeout(function () {
          attempt(remaining - 1);
        }, interval);
      }
      attempt(retries);
    });
  }

  /**
   * 序列化 DOM 元素为可传输的对象
   * @param {Element} el - DOM 元素
   * @param {number} index - 元素在列表中的索引
   * @returns {object} { __id, index, tagName, text, className, rect }
   */
  function serializeElement(el, index) {
    if (!el.__goodhr_id) {
      el.__goodhr_id =
        "el_" + Date.now() + "_" + Math.random().toString(36).substring(2, 8);
    }
    var rect = el.getBoundingClientRect();
    return {
      __id: el.__goodhr_id,
      index: index,
      tagName: el.tagName,
      text: (el.textContent || "").trim().substring(0, 2000),
      className: el.className || "",
      rect: {
        top: rect.top,
        left: rect.left,
        width: rect.width,
        height: rect.height,
      },
    };
  }

  // ════════════════════════════════════════════════
  // 3. click — DOM 点击（带等待）
  // ════════════════════════════════════════════════

  /**
   * 查找并点击元素
   * @param {string} selector - CSS 选择器
   * @param {number} index - 第几个匹配元素，默认 0
   * @param {number} retries - 重试次数，默认 3
   * @param {number} interval - 重试间隔(ms)，默认 500
   * @returns {Promise<object>} { clicked: bool, element: object|null }
   */
  function clickElement(selector, index, retries, interval) {
    index = index || 0;
    retries = retries != null ? retries : 3;
    interval = interval != null ? interval : 500;

    return new Promise(function (resolve) {
      function attempt(remaining) {
        var els = document.querySelectorAll(selector);
        var el = els[index] || null;
        if (el) {
          try {
            el.click();
            resolve({ clicked: true, element: serializeElement(el, index) });
          } catch (e) {
            resolve({ clicked: false, element: null, error: e.message });
          }
          return;
        }
        if (remaining <= 0) {
          resolve({ clicked: false, element: null, error: "元素未找到" });
          return;
        }
        setTimeout(function () {
          attempt(remaining - 1);
        }, interval);
      }
      attempt(retries);
    });
  }

  // ════════════════════════════════════════════════
  // 辅助：滚动 & 标记
  // ════════════════════════════════════════════════

  /**
   * 滚动页面到指定位置
   * @param {number} scrollY - 目标滚动位置，默认滚动到底部
   */
  function scrollPage(scrollY) {
    window.scrollTo({
      top: scrollY || document.documentElement.scrollHeight,
      behavior: "smooth",
    });
  }

  /**
   * 标记元素（视觉反馈）
   * @param {string} selector - CSS 选择器
   * @param {number} index - 第几个匹配元素
   * @param {string} reason - 标记原因
   * @param {string} markType - 标记类型 matched/rejected/error
   */
  function markElement(selector, index, reason, markType) {
    var els = document.querySelectorAll(selector);
    var el = els[index || 0];
    if (!el) return;

    var colorMap = {
      matched: "#4caf50",
      rejected: "#9e9e9e",
      error: "#f44336",
    };
    var color = colorMap[markType] || "#9e9e9e";

    el.style.outline = "2px solid " + color;
    el.style.outlineOffset = "2px";

    var label = el.querySelector(".goodhr-label");
    if (!label) {
      label = document.createElement("div");
      label.className = "goodhr-label";
      label.style.cssText =
        "position:absolute;top:0;right:0;padding:2px 8px;font-size:12px;" +
        "color:#fff;background:" +
        color +
        ";border-radius:0 0 0 4px;z-index:9999;";
      el.style.position = "relative";
      el.appendChild(label);
    }
    label.textContent = reason;
    label.style.background = color;
  }

  // ════════════════════════════════════════════════
  // 通过 __id 查找元素
  // ════════════════════════════════════════════════

  /**
   * 根据 __id 获取 DOM 元素
   * @param {string} id - 元素唯一标识
   * @returns {Element|null}
   */
  function getElementByGoodhrId(id) {
    var all = document.querySelectorAll("*");
    for (var i = 0; i < all.length; i++) {
      if (all[i].__goodhr_id === id) return all[i];
    }
    return null;
  }

  /**
   * 根据 __id 点击元素
   * @param {string} id - 元素唯一标识
   * @returns {Promise<object>} { clicked: bool }
   */
  function clickByGoodhrId(id) {
    var el = getElementByGoodhrId(id);
    if (!el) return Promise.resolve({ clicked: false, error: "元素未找到" });
    try {
      el.click();
      return Promise.resolve({
        clicked: true,
        element: serializeElement(el, 0),
      });
    } catch (e) {
      return Promise.resolve({ clicked: false, error: e.message });
    }
  }

  /**
   * 根据 __id 查找子元素内容
   * @param {string} id - 父元素唯一标识
   * @param {string} childSelector - 子元素选择器
   * @returns {object|null}
   */
  function findChildByGoodhrId(id, childSelector) {
    var el = getElementByGoodhrId(id);
    if (!el) return null;
    var child = childSelector ? el.querySelector(childSelector) : el;
    if (!child) return null;
    return serializeElement(child, 0);
  }

  /**
   * 根据 __id 标记元素
   * @param {string} id - 元素唯一标识
   * @param {string} reason - 标记原因
   * @param {string} markType - 标记类型
   */
  function markByGoodhrId(id, reason, markType) {
    var el = getElementByGoodhrId(id);
    if (!el) return;

    var colorMap = {
      matched: "#4caf50",
      rejected: "#9e9e9e",
      error: "#f44336",
    };
    var color = colorMap[markType] || "#9e9e9e";

    el.style.outline = "2px solid " + color;
    el.style.outlineOffset = "2px";
    el.__goodhr_processed = true;

    var label = el.querySelector(".goodhr-label");
    if (!label) {
      label = document.createElement("div");
      label.className = "goodhr-label";
      label.style.cssText =
        "position:absolute;top:0;right:0;padding:2px 8px;font-size:12px;" +
        "color:#fff;background:" +
        color +
        ";border-radius:0 0 0 4px;z-index:9999;";
      el.style.position = "relative";
      el.appendChild(label);
    }
    label.textContent = reason;
    label.style.background = color;
  }

  // ════════════════════════════════════════════════
  // 消息分发
  // ════════════════════════════════════════════════

  /**
   * 处理来自扩展侧的消息
   * @param {object} message - 消息对象
   * @param {chrome.runtime.MessageSender} sender
   * @param {function} sendResponse
   */
  async function handleMessage(message, sender, sendResponse) {
    if (!message || !message.action) return;

    switch (message.action) {
      case "ping":
        sendResponse({ status: "ok" });
        break;

      case "find": {
        var sel = message.selector;
        var all = message.all;
        if (all) {
          var results = await findElementAll(
            sel,
            message.retries,
            message.interval,
          );
          sendResponse({ found: results.length > 0, elements: results });
        } else {
          var el = await findElement(sel, message.retries, message.interval);
          sendResponse({ found: !!el, element: el });
        }
        break;
      }

      case "findById": {
        var byId = getElementByGoodhrId(message.id);
        if (byId) {
          var childSel = message.childSelector;
          if (childSel) {
            var child = byId.querySelector(childSel);
            sendResponse({
              found: !!child,
              element: child ? serializeElement(child, 0) : null,
            });
          } else {
            sendResponse({ found: true, element: serializeElement(byId, 0) });
          }
        } else {
          sendResponse({ found: false, element: null });
        }
        break;
      }

      case "click": {
        if (message.id) {
          var result = await clickByGoodhrId(message.id);
          sendResponse(result);
        } else {
          var result2 = await clickElement(
            message.selector,
            message.index,
            message.retries,
            message.interval,
          );
          sendResponse(result2);
        }
        break;
      }

      case "scroll": {
        scrollPage(message.scrollY);
        sendResponse({ status: "ok" });
        break;
      }

      case "mark": {
        if (message.id) {
          markByGoodhrId(message.id, message.reason, message.type);
        } else {
          markElement(
            message.selector,
            message.index,
            message.reason,
            message.type,
          );
        }
        sendResponse({ status: "ok" });
        break;
      }

      default:
        sendResponse({ status: "unknown_action", action: message.action });
        break;
    }
  }

  chrome.runtime.onMessage.addListener(
    function (message, sender, sendResponse) {
      handleMessage(message, sender, sendResponse);
      return true;
    },
  );

  sendLog("GoodHR 注入脚本已加载", "info");
})();
