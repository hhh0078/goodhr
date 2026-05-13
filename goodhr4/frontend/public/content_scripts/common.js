/**
 * common.js — 注入侧原子操作脚本
 *
 * 注入到招聘网站页面，作为扩展侧的"手和眼"。
 * 只做执行不做决策，所有业务逻辑在扩展侧完成。
 *
 * 支持跨文档查找：所有 DOM 操作会遍历主文档及可访问的 iframe 文档，
 * 确保能找到嵌入在 iframe 中的候选人卡片和详情内容。
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
  // 0. 跨文档支持 — 递归收集主文档 + iframe 文档
  // ════════════════════════════════════════════════

  /**
   * 递归收集当前页面所有可访问的文档对象（主文档 + iframe 文档）
   * 部分招聘平台（如 Boss 直聘）会将简历详情等内容嵌入 iframe 中，
   * 需要遍历 iframe 才能查找到这些元素。
   * @param {Document} doc - 起始文档，默认为当前 document
   * @param {number} maxDepth - 最大递归深度，防止无限嵌套，默认 3
   * @returns {Document[]} 文档对象数组，第一个始终是主文档
   */
  function getAllDocuments(doc, maxDepth) {
    doc = doc || document;
    maxDepth = maxDepth != null ? maxDepth : 3;
    var docs = [doc];
    if (maxDepth <= 0) return docs;

    try {
      var frames = doc.querySelectorAll("iframe");
      for (var i = 0; i < frames.length; i++) {
        try {
          var iframeDoc =
            frames[i].contentDocument || frames[i].contentWindow.document;
          if (iframeDoc) {
            docs.push(iframeDoc);
            var nested = getAllDocuments(iframeDoc, maxDepth - 1);
            for (var j = 0; j < nested.length; j++) {
              if (docs.indexOf(nested[j]) === -1) {
                docs.push(nested[j]);
              }
            }
          }
        } catch (e) {
          // 跨域 iframe 无法访问，静默跳过
        }
      }
    } catch (e) {
      // 查询 iframe 本身失败，静默跳过
    }

    return docs;
  }

  /**
   * 在所有文档中执行 querySelector，返回第一个匹配的元素及其所属文档
   * @param {string} selector - CSS 选择器
   * @returns {{ el: Element, doc: Document }|null} 匹配结果，未找到返回 null
   */
  function querySelectorAllDocs(selector) {
    var docs = getAllDocuments();
    for (var i = 0; i < docs.length; i++) {
      try {
        var el = docs[i].querySelector(selector);
        if (el) return { el: el, doc: docs[i] };
      } catch (e) {
        // 选择器无效等异常，跳过
      }
    }
    return null;
  }

  /**
   * 在所有文档中执行 querySelectorAll，合并结果
   * @param {string} selector - CSS 选择器
   * @returns {{ el: Element, doc: Document }[]} 匹配结果数组
   */
  function querySelectorAllDocsAll(selector) {
    var docs = getAllDocuments();
    var results = [];
    for (var i = 0; i < docs.length; i++) {
      try {
        var els = docs[i].querySelectorAll(selector);
        for (var j = 0; j < els.length; j++) {
          results.push({ el: els[j], doc: docs[i] });
        }
      } catch (e) {
        // 选择器无效等异常，跳过
      }
    }
    return results;
  }

  /**
   * 在所有文档中根据 __id 查找元素
   * @param {string} id - 元素唯一标识
   * @returns {Element|null} 匹配的 DOM 元素
   */
  function findElementByGoodhrId(id) {
    var docs = getAllDocuments();
    for (var d = 0; d < docs.length; d++) {
      try {
        var all = docs[d].querySelectorAll("*");
        for (var i = 0; i < all.length; i++) {
          if (all[i].__goodhr_id === id) return all[i];
        }
      } catch (e) {
        // 跨域 iframe 访问异常，跳过
      }
    }
    return null;
  }

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
  // 2. find — DOM 查找（带重试，支持跨文档）
  // ════════════════════════════════════════════════

  /**
   * 查找单个元素（带重试，跨文档查找）
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
        var result = querySelectorAllDocs(selector);
        if (result) {
          resolve(serializeElement(result.el, 0));
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
   * 查找多个元素（带重试，跨文档查找）
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
        var allResults = querySelectorAllDocsAll(selector);
        if (allResults.length > 0) {
          var results = [];
          for (var i = 0; i < allResults.length; i++) {
            results.push(serializeElement(allResults[i].el, i));
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
  // 3. click — DOM 点击（带等待，支持跨文档）
  // ════════════════════════════════════════════════

  /**
   * 查找并点击元素（跨文档查找）
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
        var allResults = querySelectorAllDocsAll(selector);
        var entry = allResults[index] || null;
        if (entry) {
          try {
            entry.el.click();
            resolve({
              clicked: true,
              element: serializeElement(entry.el, index),
            });
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
   * 标记元素（不再修改页面 DOM 样式，仅发送日志到扩展侧）
   * @param {string} selector - CSS 选择器
   * @param {number} index - 第几个匹配元素
   * @param {string} reason - 标记原因
   * @param {string} markType - 标记类型 matched/rejected/error
   */
  function markElement(selector, index, reason, markType) {
    var typeMap = {
      matched: "success",
      rejected: "info",
      error: "error",
    };
    var logType = typeMap[markType] || "info";
    sendLog("[标记] " + reason, logType);
  }

  // ════════════════════════════════════════════════
  // 通过 __id 查找元素（支持跨文档）
  // ════════════════════════════════════════════════

  /**
   * 根据 __id 获取 DOM 元素（跨文档查找）
   * @param {string} id - 元素唯一标识
   * @returns {Element|null}
   */
  function getElementByGoodhrId(id) {
    return findElementByGoodhrId(id);
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
   * 根据 __id 标记元素（不再修改页面 DOM 样式，仅发送日志到扩展侧）
   * @param {string} id - 元素唯一标识
   * @param {string} reason - 标记原因
   * @param {string} markType - 标记类型
   */
  function markByGoodhrId(id, reason, markType) {
    var typeMap = {
      matched: "success",
      rejected: "info",
      error: "error",
    };
    var logType = typeMap[markType] || "info";
    sendLog("[标记] " + reason, logType);
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

  sendLog("GoodHR 注入脚本已加载（含 iframe 跨文档支持）", "info");
})();
