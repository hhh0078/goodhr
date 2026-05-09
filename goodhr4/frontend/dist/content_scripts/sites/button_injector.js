// 通用按钮注入器 - 在document_start时运行
(function injectButton() {
  "use strict";

  // 注入通用按钮
  const script = document.createElement("script");
  script.src = chrome.runtime.getURL("utils/button-injector.js");
  script.async = false;
  
  // 注入到页面
  (document.head || document.documentElement).appendChild(script);
  script.onload = function () {
    script.parentNode && script.parentNode.removeChild(script);
  };

  script.onerror = function () {
    console.error("❌ GoodHR按钮注入失败");
  };
})();