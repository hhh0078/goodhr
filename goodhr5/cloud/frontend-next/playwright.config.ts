/** 本文件配置 Ant Design 前端回归测试，仅访问本机预览并使用模拟接口。 */
import { defineConfig } from "@playwright/test";

const baseURL = process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190";

export default defineConfig({
  testDir: "./tests/ui",
  timeout: 45_000,
  expect: { timeout: 10_000 },
  workers: 1,
  fullyParallel: false,
  reporter: [["list"]],
  outputDir: "test-results",
  use: {
    baseURL,
    channel: process.env.PLAYWRIGHT_CHANNEL || "chrome",
    headless: true,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  webServer: process.env.UI_TEST_BASE_URL
    ? undefined
    : {
        command: "pnpm exec next dev --hostname 127.0.0.1 --port 5190",
        url: baseURL,
        reuseExistingServer: !process.env.CI,
        timeout: 120_000,
      },
});
