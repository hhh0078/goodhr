// 通用按钮注入工具
(function createUniversalPluginButton() {
  "use strict";

  // 创建插件打开按钮
  function createPluginButton() {
    // 检查是否在顶层窗口中，避免在iframe中显示
    if (window.top !== window.self) {
      return;
    }

    // 检查是否已存在按钮
    if (document.getElementById("goodhr-plugin-btn")) {
      return;
    }

    // 创建按钮容器
    const buttonContainer = document.createElement("div");
    buttonContainer.id = "goodhr-plugin-btn";
    buttonContainer.style.cssText = `
      position: fixed;
      top: 20px;
      right: 20px;
      z-index: 9999;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      user-select: none;
    `;

    // 创建按钮主体
    const button = document.createElement("div");
    button.className = "goodhr-button-main";
    button.innerHTML = "GoodHR";
    button.style.cssText = `
      background: #FF85A2;
      color: white;
      padding: 8px 16px;
      border-radius: 20px;
      font-size: 14px;
      font-weight: 500;
      cursor: pointer;
      box-shadow: 0 2px 8px rgba(255, 133, 162, 0.3);
      transition: all 0.3s ease;
      display: inline-block;
      position: relative;
    `;

    // 创建关闭按钮
    const closeButton = document.createElement("div");
    closeButton.className = "goodhr-button-close";
    closeButton.innerHTML = "×";
    closeButton.style.cssText = `
      position: absolute;
      top: -8px;
      right: -8px;
      width: 20px;
      height: 20px;
      background: #ff4757;
      color: white;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 12px;
      cursor: pointer;
      opacity: 0;
      transition: opacity 0.3s ease;
      box-shadow: 0 2px 4px rgba(255, 71, 87, 0.3);
    `;

    // 添加悬停效果
    button.addEventListener("mouseenter", function () {
      this.style.background = "#FF85A2";
      this.style.transform = "translateY(-2px)";
      this.style.boxShadow = "0 4px 12px rgba(255, 133, 162, 0.4)";
      closeButton.style.opacity = "1";
    });

    button.addEventListener("mouseleave", function () {
      this.style.background = "#FF85A2";
      this.style.transform = "translateY(0)";
      this.style.boxShadow = "0 2px 8px rgba(255, 133, 162, 0.3)";
      closeButton.style.opacity = "0";
    });

    // 关闭按钮悬停效果
    closeButton.addEventListener("mouseenter", function () {
      this.style.background = "#ff3838";
      this.style.transform = "scale(1.1)";
    });

    closeButton.addEventListener("mouseleave", function () {
      this.style.background = "#ff4757";
      this.style.transform = "scale(1)";
    });

    // 点击事件 - 打开插件
    button.addEventListener("click", function () {
      // 检查是否在扩展上下文中
      if (typeof chrome !== "undefined" && chrome.runtime) {
        // 在扩展上下文中，直接发送消息
        chrome.runtime.sendMessage({ action: "OPEN_PLUGIN" });
      } else {
        // 在页面上下文中，通过自定义事件与content script通信
        const event = new CustomEvent("goodhr-open-popup", { bubbles: true });
        document.dispatchEvent(event);
      }
    });

    // 拖拽功能
    let isDragging = false;
    let dragOffsetX = 0;
    let dragOffsetY = 0;

    // 鼠标按下事件
    buttonContainer.addEventListener("mousedown", function (e) {
      // 只在点击按钮主体时才开始拖拽
      if (e.target === button || button.contains(e.target)) {
        isDragging = true;
        dragOffsetX = e.clientX - buttonContainer.offsetLeft;
        dragOffsetY = e.clientY - buttonContainer.offsetTop;
        buttonContainer.style.cursor = "grabbing";
        e.preventDefault(); // 防止选中文本
      }
    });

    // 鼠标移动事件
    document.addEventListener("mousemove", function (e) {
      if (isDragging) {
        e.preventDefault();
        const newX = e.clientX - dragOffsetX;
        const newY = e.clientY - dragOffsetY;

        // 限制在视窗内
        const maxX = window.innerWidth - buttonContainer.offsetWidth;
        const maxY = window.innerHeight - buttonContainer.offsetHeight;

        const finalX = Math.max(0, Math.min(newX, maxX));
        const finalY = Math.max(0, Math.min(newY, maxY));

        buttonContainer.style.left = finalX + "px";
        buttonContainer.style.top = finalY + "px";
      }
    });

    // 鼠标释放事件
    document.addEventListener("mouseup", function () {
      if (isDragging) {
        isDragging = false;
        buttonContainer.style.cursor = "pointer";
      }
    });

    // 触摸设备支持
    buttonContainer.addEventListener(
      "touchstart",
      function (e) {
        if (e.target === button || button.contains(e.target)) {
          const touch = e.touches[0];
          isDragging = true;
          dragOffsetX = touch.clientX - buttonContainer.offsetLeft;
          dragOffsetY = touch.clientY - buttonContainer.offsetTop;
          buttonContainer.style.cursor = "grabbing";
          e.preventDefault();
        }
      },
      { passive: false },
    );

    document.addEventListener(
      "touchmove",
      function (e) {
        if (isDragging) {
          e.preventDefault();
          const touch = e.touches[0];
          const newX = touch.clientX - dragOffsetX;
          const newY = touch.clientY - dragOffsetY;

          const maxX = window.innerWidth - buttonContainer.offsetWidth;
          const maxY = window.innerHeight - buttonContainer.offsetHeight;

          const finalX = Math.max(0, Math.min(newX, maxX));
          const finalY = Math.max(0, Math.min(newY, maxY));

          buttonContainer.style.left = finalX + "px";
          buttonContainer.style.top = finalY + "px";
        }
      },
      { passive: false },
    );

    document.addEventListener("touchend", function () {
      if (isDragging) {
        isDragging = false;
        buttonContainer.style.cursor = "pointer";
      }
    });

    // 关闭按钮点击事件
    closeButton.addEventListener("click", function (e) {
      e.stopPropagation(); // 阻止事件冒泡
      buttonContainer.remove();
    });

    // 组装元素
    button.appendChild(closeButton);
    buttonContainer.appendChild(button);

    // 添加到页面
    document.body.appendChild(buttonContainer);

    // 添加跳动动画
    buttonContainer.style.animation = "waterDrop 0.8s ease-out";

    // 创建动画样式
    if (!document.getElementById("goodhr-button-styles")) {
      const style = document.createElement("style");
      style.id = "goodhr-button-styles";
      style.textContent = `
        @keyframes waterDrop {
          0% { 
            transform: translateY(-20px) scale(0.8); 
            opacity: 0; 
          }
          30% { 
            transform: translateY(0px) scale(0.9); 
            opacity: 0.7; 
          }
          50% { 
            transform: translateY(10px) scale(1.1); 
            opacity: 0.9; 
          }
          70% { 
            transform: translateY(-5px) scale(1.05); 
            opacity: 1; 
          }
          85% { 
            transform: translateY(2px) scale(0.98); 
            opacity: 1; 
          }
          100% { 
            transform: translateY(0px) scale(1); 
            opacity: 1; 
          }
        }
      `;
      document.head.appendChild(style);
    }
  }

  // 页面加载完成后创建按钮
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", createPluginButton);
  } else {
    // 如果页面已经加载完成，延迟创建以确保DOM稳定
    setTimeout(createPluginButton, 1000);
  }
})();
