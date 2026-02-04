// 智联招聘拦截器注入器 - 在document_start时运行
(function injectZhilianInterceptor() {
  "use strict";

  // 注入通用按钮
  const buttonScript = document.createElement("script");
  buttonScript.src = chrome.runtime.getURL("utils/button-injector.js");
  buttonScript.async = false;

  // 注入到页面
  (document.head || document.documentElement).appendChild(buttonScript);
  buttonScript.onload = function () {
    buttonScript.parentNode &&
      buttonScript.parentNode.removeChild(buttonScript);
  };

  buttonScript.onerror = function () {
    console.error("❌ GoodHR按钮注入失败");
  };

  // 监听来自页面按钮的打开插件事件
  document.addEventListener("goodhr-open-popup", function () {
    chrome.runtime.sendMessage({ action: "OPEN_PLUGIN" });
  });
})();
