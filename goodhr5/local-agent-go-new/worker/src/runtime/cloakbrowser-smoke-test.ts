// 文件作用说明：安装完成后无界面试启动 CloakBrowser，并打开本地测试页验证完整浏览器链路。

import { binaryInfo, launch } from "cloakbrowser";

/** runSmokeTest 启动已缓存的 Stable Chromium，打开测试页后立即安全关闭。 */
async function runSmokeTest() {
  const licenseKey = process.env.CLOAKBROWSER_LICENSE_KEY?.trim() ?? "";
  if (!licenseKey) {
    throw new Error("CloakBrowser Key 为空");
  }
  const info = binaryInfo(undefined, "stable");
  if (!info.installed) {
    throw new Error("官方 Chromium 还没有安装完成");
  }
  const browser = await launch({
    licenseKey,
    releaseChannel: "stable",
    headless: true,
    humanize: false,
  });
  try {
    const page = await browser.newPage();
    await page.goto(
      "data:text/html,<title>GoodHR Browser Check</title><main>ready</main>",
      { waitUntil: "domcontentloaded", timeout: 15_000 },
    );
    const title = await page.title();
    if (title !== "GoodHR Browser Check") {
      throw new Error("测试页面没有正确打开");
    }
    process.stdout.write(
      `${JSON.stringify({ ok: true, version: info.version, binary_path: info.binaryPath })}\n`,
    );
  } finally {
    await browser.close();
  }
}

runSmokeTest().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exitCode = 1;
});
