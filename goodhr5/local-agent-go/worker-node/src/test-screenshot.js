/**
 * 测试脚本：验证详情滚动截图功能。
 * 启动 Worker 后运行：node src/test-screenshot.js
 */
const path = require("path");
const fs = require("fs");
const os = require("os");

async function main() {
  console.log("===== 详情滚动截图测试 =====");
  
  // 启动 Playwright 浏览器
  const { chromium } = require("playwright-core");
  const browser = await chromium.launch({ headless: false, channel: "chrome" });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 900 },
  });
  const page = await context.newPage();
  
  try {
    // 打开 Boss 直聘网站
    console.log("[测试] 打开 Boss 直聘...");
    await page.goto("https://www.zhipin.com", { waitUntil: "domcontentloaded", timeout: 15000 });
    await page.waitForTimeout(2000);
    
    // 尝试打开详情（如果有候选人卡片的话）
    // 先尝试直接模拟一个详情弹框来测试
    console.log("[测试] 打开聊天/详情页...");
    
    // 尝试进入 webchat 页面（模拟查看候选人详情）
    const currentUrl = page.url();
    console.log("[测试] 当前页面:", currentUrl);
    
    // 查找候选人卡片
    const candidateCards = page.locator(".candidate-card, .geek-card, [class*='card'], li:has([class*='name'])");
    const cardCount = await candidateCards.count();
    console.log("[测试] 找到候选人卡片数量:", cardCount);
    
    if (cardCount > 0) {
      // 点击第一个卡片
      console.log("[测试] 点击第一个候选人卡片");
      await candidateCards.first().click({ timeout: 3000 });
      await page.waitForTimeout(1500);
    }
    
    // 查找详情容器
    const detailSelectors = [
      ".dialog-resume-wrapper",
      ".resume-wrapper",
      ".detail-container",
      ".geek-resume-wrapper",
      ".resume-main",
      "[class*='resume'][class*='detail']",
      "[class*='dialog'][class*='resume']",
      ".detail-modal",
    ];
    
    let detailLocator = null;
    for (const sel of detailSelectors) {
      try {
        const loc = page.locator(sel).first();
        if ((await loc.count()) > 0 && await loc.isVisible()) {
          console.log("[测试] 找到详情容器选择器:", sel);
          detailLocator = loc;
          break;
        }
      } catch(e) {
        console.log("[测试] 选择器", sel, "出错:", e.message);
      }
    }
    
    if (!detailLocator) {
      console.log("[测试] 未找到详情容器，尝试截图整个页面");
      const pageShot = await page.screenshot({ path: "/tmp/test-page-full.png", fullPage: true });
      console.log("[测试] 全页截图:", pageShot.length, "bytes");
    } else {
      const box = await detailLocator.boundingBox();
      console.log("[测试] 详情容器 box:", JSON.stringify({ x: box.x, y: box.y, w: box.width, h: box.height }));
      
      const viewport = page.viewportSize();
      console.log("[测试] 视口尺寸:", JSON.stringify(viewport));
      
      // 检查容器滚动信息
      const scrollInfo = await detailLocator.evaluate((el) => {
        const style = window.getComputedStyle(el);
        const overflowY = style.overflowY || "";
        const scrollHeight = Math.ceil(el.scrollHeight || 0);
        const clientHeight = Math.ceil(el.clientHeight || 0);
        return {
          overflowY,
          scrollHeight,
          clientHeight,
          scrollable: scrollHeight > clientHeight + 8 && !["hidden", "clip"].includes(overflowY),
          scrollTop: Math.round(el.scrollTop || 0),
        };
      });
      console.log("[测试] 容器滚动信息:", JSON.stringify(scrollInfo));
      
      // 检查容器内部是否有更多内容可滚动
      const innerElements = await detailLocator.evaluate((el) => {
        const children = el.children;
        const info = [];
        for (let i = 0; i < children.length; i++) {
          const child = children[i];
          info.push({
            tag: child.tagName,
            scrollHeight: Math.ceil(child.scrollHeight || 0),
            clientHeight: Math.ceil(child.clientHeight || 0),
            offsetTop: child.offsetTop,
            offsetHeight: child.offsetHeight,
          });
        }
        return info;
      });
      console.log("[测试] 容器内部元素信息:");
      innerElements.forEach((e, i) => {
        console.log(`  [${i}] tag=${e.tag} scrollH=${e.scrollHeight} clientH=${e.clientHeight} top=${e.offsetTop} offsetH=${e.offsetHeight}`);
      });
      
      // 尝试分段滚动截图
      const maxScrolls = 10;
      const clipX = Math.max(Math.round(box.x), 0);
      const clipY = Math.max(Math.round(box.y), 0);
      const clipWidth = Math.max(Math.round(box.width), 1);
      const clipBottom = Math.min(Math.round(box.y + box.height), Math.round(viewport.height));
      const clipHeight = Math.max(clipBottom - clipY, 1);
      const scrollDelta = Math.max(Math.round(clipHeight * 0.7), 1);
      
      console.log("[测试] 截图参数:", JSON.stringify({ clipX, clipY, clipWidth, clipHeight, scrollDelta }));
      
      const dir = "/tmp/test-screenshots";
      await fs.promises.mkdir(dir, { recursive: true });
      
      let prevBuffer = null;
      for (let i = 0; i < maxScrolls; i++) {
        // 如果容器可内部滚动，用 scrollTop
        if (scrollInfo.scrollable) {
          const top = Math.min(i * scrollDelta, Math.max(scrollInfo.scrollHeight - scrollInfo.clientHeight, 0));
          await detailLocator.evaluate((el, y) => { el.scrollTop = y; }, top);
          await page.waitForTimeout(300);
        } else if (i > 0) {
          // 否则滚动画布
          await page.mouse.move(clipX + clipWidth / 2, clipY + clipHeight / 2);
          await page.mouse.wheel(0, scrollDelta);
          await page.waitForTimeout(500);
        }
        
        const shotPath = path.join(dir, `detail-part-${i+1}.png`);
        await page.screenshot({ path: shotPath, clip: { x: clipX, y: clipY, width: clipWidth, height: clipHeight }, type: "png" });
        const stat = await fs.promises.stat(shotPath);
        const curBuffer = await fs.promises.readFile(shotPath);
        
        let isDup = false;
        if (prevBuffer && prevBuffer.length === curBuffer.length) {
          const step = Math.max(1, Math.floor(prevBuffer.length / 2400));
          let same = 0, total = 0;
          for (let j = 0; j < prevBuffer.length && j < curBuffer.length; j += step) {
            total++;
            if (Math.abs(prevBuffer[j] - curBuffer[j]) <= 2) same++;
          }
          isDup = total > 0 && same / total >= 0.985;
        }
        
        console.log(`[测试] 截图 ${i+1}/${maxScrolls} size=${stat.size} bytes${isDup ? ' [重复-终止]' : ''}`);
        
        if (isDup) break;
        prevBuffer = curBuffer;
        
        const curScrollTop = scrollInfo.scrollable 
          ? await detailLocator.evaluate((el) => el.scrollTop)
          : -1;
        console.log(`[测试] 当前 scrollTop=${curScrollTop}, 剩余=${scrollInfo.scrollable ? Math.max(scrollInfo.scrollHeight - scrollInfo.clientHeight - curScrollTop, 0) : 'N/A'}`);
        
        if (scrollInfo.scrollable && curScrollTop >= scrollInfo.scrollHeight - scrollInfo.clientHeight - 2) {
          console.log("[测试] 已滚动到底部");
          break;
        }
      }
      
      console.log("[测试] 所有截图保存在:", dir);
      const files = fs.readdirSync(dir);
      console.log("[测试] 文件列表:", files);
    }
    
    console.log("===== 测试完成 =====");
  } catch (err) {
    console.error("[测试] 出错:", err.message);
    console.error(err.stack);
  } finally {
    await page.waitForTimeout(3000);
    await browser.close();
  }
}

main().catch(console.error);
