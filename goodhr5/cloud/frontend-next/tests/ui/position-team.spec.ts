/** 文件作用说明：用合成团队岗位验证名称搜索、管理员代改和成员复制，全部业务接口被拦截，不修改真实数据。 */
import { test, expect } from "@playwright/test";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "team-position-synthetic-token" }] }] } });
const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };

for (const width of [1366, 390]) {
  for (const admin of [true, false]) {
    test(`${width}px${admin ? "管理员代改" : "普通成员复制"}及岗位搜索`, async ({ page }, info) => {
      await page.setViewportSize({ width, height: 900 });
      const state = await mockUniformAdmin(page);
      const writes: any[] = [];
      const source = { id: "team-source", name: "Java工程师（团队岗位）", platform_id: "boss", creator_email: "colleague@example.invalid", is_current_user: false, can_edit: admin, is_open: true, status: "running", scanned_count: 99, today_greeted_count: 8, common_config: { request_phone: true }, ai_config: { position_requirement: "三年Java经验", greet_score_threshold: 85 }, match_limit: 30 };
      const positions = [source, { ...source, id: "own-position", name: "我的设计岗位", creator_email: "uniform@example.invalid", is_current_user: true, can_edit: true, status: "stopped" }, { ...source, id: "closed-position", name: "关闭的Java岗位", is_open: false }];
      await page.route("**/api/positions?**", route => {
        const closed = new URL(route.request().url()).searchParams.get("is_open") === "false";
        return route.fulfill({ headers, json: { positions: positions.filter(item => item.is_open !== closed) } });
      });
      await page.route("**/api/positions", async route => {
        if (route.request().method() !== "POST") return route.fallback();
        const body = route.request().postDataJSON();
        writes.push(body);
        const saved = { ...body, id: body.id || "copied-position", creator_email: body.id ? source.creator_email : "uniform@example.invalid", is_current_user: !body.id, can_edit: true, status: body.id ? "running" : "created" };
        if (body.id) Object.assign(source, saved); else positions.push(saved);
        await route.fulfill({ headers, json: { position: saved } });
      });
      await page.goto("/admin/positions");
      const toolbar = page.getByTestId("positions-toolbar");
      const search = toolbar.getByRole("textbox", { name: "搜索岗位名称", exact: true });
      await expect(toolbar.getByRole("button", { name: "新建岗位", exact: true })).toBeInViewport();
      await search.fill("  jAvA  ");
      await expect(page.locator('[data-position-id="team-source"]').first()).toBeVisible();
      await expect(page.locator('.ant-card[data-position-id="own-position"]')).toHaveCount(0);
      await search.fill("没有这个岗位");
      await expect(page.getByText("没找到这个岗位，换个关键词，或清空搜索再看看。", { exact: true })).toBeVisible();
      await search.fill("");
      await toolbar.getByText("关闭", { exact: true }).click();
      await expect(page.locator('.ant-card[data-position-id="closed-position"]')).toBeVisible();
      await toolbar.getByText("开启", { exact: true }).click();
      const card = page.locator('.ant-card[data-position-id="team-source"]');
      await expect(card.getByRole("button", { name: "开始", exact: true })).toHaveCount(0);
      await expect(card.getByRole("button", { name: "停止", exact: true })).toHaveCount(0);
      await expect(card.getByRole("button", { name: "删除", exact: true })).toHaveCount(0);
      await expect(card.getByRole("button", { name: "编辑团队岗位", exact: true })).toHaveCount(admin ? 1 : 0);
      await expect(card.getByRole("button", { name: "复制到我的岗位", exact: true })).toBeInViewport();
      await page.screenshot({ path: info.outputPath(`team-positions-${width}-${admin}.png`), fullPage: true, animations: "disabled" });
      if (admin) {
        await card.getByRole("button", { name: "编辑团队岗位", exact: true }).click();
        const editor = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
        await editor.getByRole("textbox", { name: "岗位名称", exact: true }).fill("Java工程师（管理员代改）");
        await editor.getByRole("button", { name: "保存修改", exact: true }).click();
        await expect(editor).toBeHidden();
        expect(writes[0].id).toBe("team-source");
      }
      await card.getByRole("button", { name: "复制到我的岗位", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "新建岗位模板", exact: true });
      await expect(dialog.getByRole("textbox", { name: "岗位名称", exact: true })).toHaveValue(source.name);
      await expect(dialog.getByRole("switch", { name: "岗位开启状态", exact: true })).toBeChecked();
      await dialog.getByRole("textbox", { name: "岗位名称", exact: true }).fill("Java工程师（我的副本）");
      await dialog.getByRole("button", { name: "创建岗位", exact: true }).click();
      await expect(dialog).toBeHidden();
      const copied = writes.at(-1);
      expect(copied.id).toBe("");
      expect(copied.ai_config.position_requirement).toBe("三年Java经验");
      expect(copied.ai_config.greet_score_threshold).toBe(85);
      expect(copied.common_config.request_phone).toBe(true);
      expect(copied.match_limit).toBe(30);
      expect(copied).not.toHaveProperty("status");
      expect(copied).not.toHaveProperty("scanned_count");
      expect(copied).not.toHaveProperty("creator_email");
      await expect(page.locator('.ant-card[data-position-id="copied-position"]')).toContainText("Java工程师（我的副本）");
      expect(source.name).not.toBe("Java工程师（我的副本）");
      expect(state.errors).toEqual([]);
    });
  }
}
