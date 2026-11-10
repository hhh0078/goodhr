/** 文件作用说明：以模拟旧岗位验证桌面和手机启动前提醒，确保不会打开招聘平台、启动任务或删除岗位。 */
import { test, expect } from "@playwright/test";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "legacy-synthetic-token" }] }] } });
const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };

for (const width of [1366, 390]) {
  for (const legacy of ["marker", "keyword", "ocr"]) {
    test(`${width}px旧岗位${legacy}先提醒重建且无启动或删除请求`, async ({ page }, info) => {
      await page.setViewportSize({ width, height: 900 });
      const state = await mockUniformAdmin(page);
      await page.route("**/api/positions?**", route => route.fulfill({ headers, json: { positions: [{
        id: "synthetic-legacy", name: "合成旧岗位", platform_id: "boss", creator_email: "uniform@example.invalid", is_open: true, status: "stopped",
        requires_recreate: legacy === "marker",
        common_config: { mode_default: legacy === "keyword" ? "keyword" : "ai", detail_mode: legacy === "ocr" ? "ocr" : "ai" },
      }], total: 1 } }));
      await page.goto("/admin/positions");
      await page.getByRole("button", { name: "开始", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "这个岗位需要重新创建", exact: true });
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText("请先复制岗位名称和要求，再删除该岗位并重新创建");
      await expect(page.getByRole("dialog", { name: "开始招聘岗位", exact: true })).toHaveCount(0);
      await expect(dialog.getByRole("button", { name: "我知道了", exact: true })).toBeInViewport();
      if (legacy === "marker") await page.screenshot({ path: info.outputPath(`legacy-warning-${width}.png`), fullPage: true, animations: "disabled" });
      const box = await dialog.boundingBox();
      expect(box && box.x >= 0 && box.x + box.width <= width).toBeTruthy();
      await dialog.getByRole("button", { name: "我知道了", exact: true }).click();
      await expect(dialog).toBeHidden();
      expect(state.calls.filter(call => /\/(run|start|page\/open)$/.test(call.path) || call.method === "DELETE")).toEqual([]);
      expect(state.errors).toEqual([]);
    });
  }
}
