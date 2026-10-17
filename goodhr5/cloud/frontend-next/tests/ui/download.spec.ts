/** 文件作用说明：用合成公开配置验证下载分区、近期更新、展开历史和无脚本预渲染内容。 */
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const fixtureOrigin = `http://127.0.0.1:${process.env.DOWNLOAD_FIXTURE_PORT || "5239"}`;

/** setFirefoxReleaseMode 切换合成接口的旧签名包或最新临时清单状态。 */
async function setFirefoxReleaseMode(mode: "signed" | "unsigned" | "temporary-unavailable" | "temporary-invalid") {
  const response = await fetch(`${fixtureOrigin}/__test__/firefox-release?mode=${mode}`);
  if (!response.ok) throw new Error(`切换 Firefox 合成配置失败：${response.status}`);
}

test("下载入口分区，更新记录默认三条并可展开收起", async ({ page }) => {
  await setFirefoxReleaseMode("signed");
  await page.goto("/download");
  const local = page.locator("#local-agent");
  const extension = page.locator("#browser-extension");
  const installGuide = page.getByRole("dialog", {
    name: "安装浏览器插件",
    exact: true,
  });
  const windowsGuide = page.getByRole("dialog", {
    name: "安装 Windows 版 GoodHR",
    exact: true,
  });
  const macGuide = page.getByRole("dialog", {
    name: "安装 macOS 版 GoodHR",
    exact: true,
  });
  const history = page.getByRole("region", { name: "本地程序更新记录", exact: true });
  await expect(local.getByRole("heading", { name: "本地程序下载", exact: true, level: 1 })).toBeVisible();
  await expect(page.getByRole("heading", { name: "下载并安装 GoodHR", exact: true })).toHaveCount(0);
  await expect(page.getByRole("navigation", { name: "下载分类", exact: true })).toHaveCount(0);
  const windowsCard = local.locator(".public-panel").filter({ hasText: "Windows" });
  const windowsIconBox = await windowsCard.locator(".anticon-windows").boundingBox();
  const windowsTitleBox = await windowsCard.getByRole("heading", { name: "Windows", exact: true }).boundingBox();
  expect(windowsIconBox).not.toBeNull();
  expect(windowsTitleBox).not.toBeNull();
  expect(Math.abs((windowsIconBox!.y + windowsIconBox!.height / 2) - (windowsTitleBox!.y + windowsTitleBox!.height / 2))).toBeLessThan(4);
  expect((await windowsCard.boundingBox())!.height).toBeLessThan(220);
  const windowsDownload = windowsCard.getByRole("link", { name: "下载安装程序", exact: true });
  await expect(windowsDownload).toHaveAttribute("href", "https://example.invalid/goodhr-windows.zip");
  await windowsDownload.click();
  await expect(windowsGuide).toBeVisible();
  await expect(windowsGuide).toContainText("360、安全管家");
  await expect(windowsGuide).toContainText("直接打开下载好的压缩包");
  await expect(windowsGuide).toContainText("双击安装程序");
  await expect(windowsGuide).toContainText("无需先解压");
  await expect(windowsGuide).toContainText("联系作者协助");
  await expect(windowsGuide.getByRole("link", { name: "下载 Windows 版", exact: true })).toHaveAttribute("href", "https://example.invalid/goodhr-windows.zip");
  await windowsGuide.getByRole("button", { name: "先不下载", exact: true }).click();
  await expect(windowsGuide).not.toBeVisible();
  const macCard = local.locator(".public-panel").filter({ hasText: "macOS" });
  const macDownload = macCard.getByRole("link", { name: "下载安装程序", exact: true });
  await expect(macDownload).toHaveAttribute("href", "https://example.invalid/goodhr-mac.zip");
  await macDownload.click();
  await expect(macGuide).toBeVisible();
  await expect(macGuide).toContainText("首次打开 macOS 安装包或程序");
  await expect(macGuide).toContainText("移到废纸篓");
  await expect(macGuide).toContainText("系统设置 → 隐私与安全性");
  await expect(macGuide).toContainText("仍要打开");
  await expect(macGuide).toContainText("联系作者协助");
  await expect(macGuide.getByRole("link", { name: "下载 macOS 版", exact: true })).toHaveAttribute("href", "https://example.invalid/goodhr-mac.zip");
  await macGuide.getByRole("button", { name: "先不下载", exact: true }).click();
  await expect(macGuide).not.toBeVisible();
  await expect(extension.getByRole("heading", { name: "浏览器插件下载", exact: true })).toBeVisible();
  await expect(extension.locator(".public-panel").filter({ has: page.getByRole("heading", { name: "Chrome", exact: true }) })).toContainText("当前版本 1.0.4。");
  await expect(extension.locator(".public-panel").filter({ has: page.getByRole("heading", { name: "Firefox", exact: true }) })).toContainText("当前版本 1.0.16。");
  await expect(extension.getByText(/当前支持：BOSS直聘、猎聘企业端、猎聘猎头端、智联招聘、前程无忧/)).toBeVisible();
  const chromeDownload = extension.getByRole("link", {
    name: "下载 Chrome 插件",
    exact: true,
  });
  await expect(installGuide).not.toBeVisible();
  await expect(chromeDownload).toHaveAttribute(
    "href",
    "/downloads/goodhr-extension-chrome-1.0.4.zip",
  );
  await chromeDownload.click();
  await expect(installGuide).toBeVisible();
  await expect(
    installGuide.getByRole("heading", { name: "安装方法", exact: true }),
  ).toBeVisible();
  await expect(
    installGuide.getByRole("heading", { name: "使用方法", exact: true }),
  ).toBeVisible();
  await expect(installGuide).toContainText("开发者模式");
  await expect(installGuide).toContainText("无需解压");
  await expect(installGuide).toContainText("直接拖到扩展管理页");
  await expect(installGuide).not.toContainText("加载已解压的扩展程序");
  await expect(installGuide).toContainText("登录你的 GoodHR 账号");
  await expect(installGuide).toContainText("当前支持 BOSS直聘、猎聘企业端、猎聘猎头端、智联招聘、前程无忧");
  await expect(installGuide).toContainText("助手会自动识别并打开");
  await expect(installGuide).toContainText("手机号是入库必填项");
  await expect(
    installGuide.getByRole("link", {
      name: "前往插件下载",
      exact: true,
    }),
  ).toHaveAttribute("href", "/downloads/goodhr-extension-chrome-1.0.4.zip");
  await installGuide
    .getByRole("button", { name: "先不安装", exact: true })
    .click();
  await expect(installGuide).not.toBeVisible();
  const firefoxDownload = extension.getByRole("link", {
    name: "下载 Firefox 插件",
    exact: true,
  });
  await expect(firefoxDownload).toHaveAttribute(
    "href",
    `${fixtureOrigin}/zip/goodhr-resume-firefox-unsigned-1.0.16.xpi`,
  );
  await expect(firefoxDownload).toHaveAttribute("download", "");
  await firefoxDownload.click();
  await expect(installGuide).toBeVisible();
  await expect(installGuide).toContainText("GoodHR 自带火狐会自动更新并加载");
  await expect(installGuide).toContainText("about:debugging#/runtime/this-firefox");
  await expect(installGuide).toContainText("临时载入附加组件");
  await expect(installGuide).toContainText("关闭后需重新加载");
  await expect(installGuide).not.toContainText("经过 Mozilla 签名");
  await expect(installGuide.getByRole("link", { name: "下载 Firefox 插件", exact: true })).toHaveAttribute("download", "");
  await installGuide.getByRole("button", { name: "先不安装", exact: true }).click();
  await expect(history.getByRole("article")).toHaveCount(3);
  await expect(history.getByText("synthetic-40", { exact: true })).toBeVisible();
  await expect(history.getByText("synthetic-37", { exact: true })).toHaveCount(0);
  await history.getByRole("button", { name: "展示更多", exact: true }).click();
  await expect(history.getByRole("article")).toHaveCount(40);
  await expect(history.getByText("synthetic-1", { exact: true })).toBeVisible();
  await history.getByRole("button", { name: "收起", exact: true }).click();
  await expect(history.getByRole("article")).toHaveCount(3);
  await expect(history.getByRole("button", { name: "展示更多", exact: true })).toHaveAttribute("aria-expanded", "false");
  mkdirSync(resolve("artifacts/resume-library"), { recursive: true });
  await page.screenshot({ path: resolve("artifacts/resume-library/download-groups.png"), fullPage: true });
});

test("不启用JavaScript时下载入口和近期更新仍有服务端正文", async ({ browser, baseURL }) => {
  await setFirefoxReleaseMode("signed");
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(`${baseURL}/download`);
  await expect(page.locator("#local-agent")).toContainText("Windows");
  const local = page.locator("#local-agent");
  await expect(local.getByRole("link", { name: "下载安装程序", exact: true })).toHaveCount(2);
  await expect(local.getByRole("link", { name: "下载安装程序", exact: true }).first()).toHaveAttribute("href", "https://example.invalid/goodhr-windows.zip");
  await expect(local.getByRole("link", { name: "下载安装程序", exact: true }).nth(1)).toHaveAttribute("href", "https://example.invalid/goodhr-mac.zip");
  const extension = page.locator("#browser-extension");
  await expect(extension).toContainText("Chrome");
  await expect(extension).toContainText("开发者模式");
  await expect(extension).toContainText("无需解压");
  await expect(
    extension.getByRole("link", { name: "下载 Chrome 插件", exact: true }),
  ).toHaveAttribute("href", "/downloads/goodhr-extension-chrome-1.0.4.zip");
  await expect(
    extension.getByRole("link", { name: "下载 Firefox 插件", exact: true }),
  ).toHaveAttribute("href", `${fixtureOrigin}/zip/goodhr-resume-firefox-unsigned-1.0.16.xpi`);
  await expect(extension).toContainText("当前版本 1.0.16。");
  await expect(page.getByRole("article")).toHaveCount(3);
  await expect(page.getByRole("heading", { name: "synthetic-40", exact: true })).toBeVisible();
  await context.close();
});

test("旧签名通道不可用时仍下载新版临时 Firefox 包", async ({ page }) => {
  await setFirefoxReleaseMode("unsigned");
  try {
    await page.goto("/download");
    const extension = page.locator("#browser-extension");
    await expect(extension).toContainText("当前版本 1.0.16。");
    await expect(extension.getByRole("link", { name: "下载 Firefox 插件", exact: true })).toHaveAttribute("href", `${fixtureOrigin}/zip/goodhr-resume-firefox-unsigned-1.0.16.xpi`);
  } finally {
    await setFirefoxReleaseMode("signed");
  }
});

test("最新 Firefox 清单失效时明确停用下载，不退回旧签名版本", async ({ page }) => {
  try {
    for (const mode of ["temporary-unavailable", "temporary-invalid"] as const) {
      await setFirefoxReleaseMode(mode);
      await page.goto("/download");
      const extension = page.locator("#browser-extension");
      await expect(extension).toContainText("暂时没读到最新插件，请稍后重试。");
      await expect(extension.getByRole("link", { name: "下载 Firefox 插件", exact: true })).toHaveCount(0);
      await expect(extension).not.toContainText("当前版本 1.0.3。");
      await expect(extension.getByText("正在准备中", { exact: true })).toBeVisible();
    }
  } finally {
    await setFirefoxReleaseMode("signed");
  }
});
