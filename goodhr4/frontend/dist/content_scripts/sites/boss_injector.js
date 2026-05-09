// Boss直聘拦截器注入器 - 在document_start时运行
(function injectBossInterceptor() {
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

  // 监听来自页面按钮的打开插件事件
  document.addEventListener("goodhr-open-popup", function () {
    chrome.runtime.sendMessage({ action: "OPEN_PLUGIN" });
  });

  // 创建script元素注入拦截器
  const interceptorScript = document.createElement("script");
  interceptorScript.src = chrome.runtime.getURL(
    "content_scripts/sites/boss_interceptor.js",
  );
  interceptorScript.async = false;

  // 注入到页面
  (document.head || document.documentElement).appendChild(interceptorScript);
  interceptorScript.onload = function () {
    interceptorScript.parentNode &&
      interceptorScript.parentNode.removeChild(interceptorScript);
  };

  interceptorScript.onerror = function () {
    console.error("❌ Boss直聘API拦截器注入失败");
  };
})();
