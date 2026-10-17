/** 文件作用说明：使用合成接口验证推荐与面试列表的头像、缺图回退、姓名按钮及桌面/手机紧凑布局；不读取真实简历。 */
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { expectUniformNetwork, mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const output = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "../../../output/list-avatars-20261017");
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "list-avatar-synthetic-token" }] }] } });

for (const width of [1440, 390]) {
  for (const route of ["recommendations", "recruitment"]) {
    test(`${width}px ${route} 列表展示头像，缺图回退并保留姓名按钮`, async ({ page }) => {
      const state = await mockUniformAdmin(page);
      const externalImages: string[] = [];
      page.on("request", (request) => { if (request.url().includes("avatar.example.invalid")) externalImages.push(request.url()); });
      await page.setViewportSize({ width, height: 960 });
      await page.goto(`/admin/${route}`);
      const first = page.locator(".ant-table-row").filter({ has: page.getByRole("button", { name: "合成候选人1", exact: true }) }).first();
      const avatar = first.getByRole("img", { name: "合成候选人1头像", exact: true });
      const name = first.getByRole("button", { name: "合成候选人1", exact: true });
      await expect(name).toBeVisible();
      await expect(avatar).toBeVisible();
      await expect(first.locator(".ant-avatar-string")).toHaveCount(0);
      const [avatarBox, nameBox] = await Promise.all([first.locator(".ant-avatar").boundingBox(), name.boundingBox()]);
      if (!avatarBox || !nameBox) throw new Error("头像或姓名尺寸没有读到。");
      expect(avatarBox.width).toBe(32);
      expect(avatarBox.height).toBe(32);
      // 推荐姓名下方保留手机号；整个文字组与头像垂直居中，面试单姓名直接居中。
      const summary = route === "recommendations" ? name.locator("..").first() : name;
      const summaryBox = await summary.boundingBox();
      if (!summaryBox) throw new Error("候选人文字区域尺寸没有读到。");
      expect(Math.abs(avatarBox.y + avatarBox.height / 2 - summaryBox.y - summaryBox.height / 2)).toBeLessThanOrEqual(1);
      for (const index of [2, 3, 4]) {
        const fallback = page.locator(".ant-table-row").filter({ has: page.getByRole("button", { name: `合成候选人${index}`, exact: true }) }).first();
        await expect(fallback.locator(".ant-avatar-string")).toHaveText("合");
        await expect(fallback.getByRole("img")).toHaveCount(0);
        await expect(fallback.getByRole("button", { name: `合成候选人${index}`, exact: true })).toBeEnabled();
      }
      expect(externalImages).toEqual([]);
      expectUniformNetwork(state);
      mkdirSync(output, { recursive: true });
      await page.screenshot({ path: resolve(output, `${route}-${width}.png`), fullPage: true, animations: "disabled" });
    });
  }
}
