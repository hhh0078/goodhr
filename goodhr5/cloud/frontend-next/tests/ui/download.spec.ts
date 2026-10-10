/** 文件作用说明：用合成公开配置验证下载分区、近期更新、展开历史和无脚本预渲染内容。 */
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

test("下载入口分区，更新记录默认三条并可展开收起", async ({ page }) => {
  await page.goto("/download");
  const local = page.locator("#local-agent");
  const extension = page.locator("#browser-extension");
  const history = page.getByRole("region", { name: "本地程序更新记录", exact: true });
  await expect(local.getByRole("heading", { name: "本地程序下载", exact: true })).toBeVisible();
  await expect(extension.getByRole("heading", { name: "浏览器插件下载 · v1.0.3", exact: true })).toBeVisible();
  await expect(extension.getByRole("link", { name: /下载 Chrome 插件/ })).toHaveAttribute("href", "/downloads/goodhr-extension-chrome-1.0.3.zip");
  await expect(extension.getByRole("button", { name: "正在准备中", exact: true })).toBeDisabled();
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
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(`${baseURL}/download`);
  await expect(page.locator("#local-agent")).toContainText("Windows");
  await expect(page.locator("#browser-extension")).toContainText("Chrome");
  await expect(page.getByRole("article")).toHaveCount(3);
  await expect(page.getByRole("heading", { name: "synthetic-40", exact: true })).toBeVisible();
  await context.close();
});
