/** 文件作用说明：用合成公开配置验证下载分区、近期更新、展开历史和无脚本预渲染内容。 */
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const fixtureOrigin = `http://127.0.0.1:${process.env.DOWNLOAD_FIXTURE_PORT || "5239"}`;

/** setFirefoxReleaseMode 切换合成接口的 Firefox 签名状态。 */
async function setFirefoxReleaseMode(mode: "signed" | "unsigned") {
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
  const history = page.getByRole("region", { name: "本地程序更新记录", exact: true });
  await expect(local.getByRole("heading", { name: "本地程序下载", exact: true })).toBeVisible();
  await expect(extension.getByRole("heading", { name: "浏览器插件下载 · v1.0.3", exact: true })).toBeVisible();
  const chromeDownload = extension.getByRole("link", {
    name: "下载 Chrome 插件",
    exact: true,
  });
  await expect(installGuide).not.toBeVisible();
  await expect(chromeDownload).toHaveAttribute(
    "href",
    "/downloads/goodhr-extension-chrome-1.0.3.zip",
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
  await expect(installGuide).toContainText("登录你的 GoodHR 账号");
  await expect(installGuide).toContainText("助手会自动识别并打开");
  await expect(installGuide).toContainText("手机号是入库必填项");
  await expect(
    installGuide.getByRole("link", {
      name: "前往插件下载",
      exact: true,
    }),
  ).toHaveAttribute("href", "/downloads/goodhr-extension-chrome-1.0.3.zip");
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
    "https://downloads.example.invalid/goodhr-extension-firefox-1.0.3.xpi",
  );
  await firefoxDownload.click();
  await expect(installGuide).toBeVisible();
  await expect(installGuide).toContainText("经过 Mozilla 签名");
  await expect(installGuide).not.toContainText("临时载入附加组件");
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
  const extension = page.locator("#browser-extension");
  await expect(extension).toContainText("Chrome");
  await expect(extension).toContainText("开发者模式");
  await expect(
    extension.getByRole("link", { name: "下载 Chrome 插件", exact: true }),
  ).toHaveAttribute("href", "/downloads/goodhr-extension-chrome-1.0.3.zip");
  await expect(
    extension.getByRole("link", { name: "下载 Firefox 插件", exact: true }),
  ).toHaveAttribute("href", "https://downloads.example.invalid/goodhr-extension-firefox-1.0.3.xpi");
  await expect(page.getByRole("article")).toHaveCount(3);
  await expect(page.getByRole("heading", { name: "synthetic-40", exact: true })).toBeVisible();
  await context.close();
});

test("未签名 Firefox 包只显示签名进度且没有下载入口", async ({ page }) => {
  await setFirefoxReleaseMode("unsigned");
  try {
    await page.goto("/download");
    const extension = page.locator("#browser-extension");
    await expect(extension).toContainText("正在完成 Mozilla 签名，签名通过后开放下载。");
    await expect(extension.getByRole("link", { name: "下载 Firefox 插件", exact: true })).toHaveCount(0);
    await expect(extension.getByText("正在准备中", { exact: true })).toBeVisible();
    await expect(page.getByText("临时载入附加组件", { exact: true })).toHaveCount(0);
  } finally {
    await setFirefoxReleaseMode("signed");
  }
});
