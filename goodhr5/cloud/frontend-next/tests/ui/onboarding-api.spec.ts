/** 文件作用：用实际本地 Go 后端验证教程完成事实，确保扫描数量、跳过和非终态分析不会伪造完成。 */
import { test, expect } from "@playwright/test";
import { CLOUD_URL, readUserFlow, registerUser } from "../fixtures/onboarding-helpers";

test("新用户可先保存岗位和对应平台，创建本身不冒充教程跑通", async ({ request }) => {
  const user = await registerUser(request, "onboarding-position-before-login");
  const headers = { authorization: `Bearer ${user.token}` };
  const created = await request.post(`${CLOUD_URL}/api/positions`, { headers, data: { name: "先建岗位再登录平台", platform_id: "zhaopin", is_open: true, keywords: ["招聘"], common_config: { mode_default: "keyword", detail_mode: "dom" }, ai_config: {} } });
  expect(created.ok(), await created.text()).toBeTruthy();
  const position = (await created.json()).position;
  expect(position.id).toBeTruthy();
  expect(position.platform_id).toBe("zhaopin");
  expect(position.common_config.detail_mode).toBe("dom");
  const listed = await request.get(`${CLOUD_URL}/api/positions`, { headers });
  expect(listed.ok()).toBeTruthy();
  expect((await listed.json()).positions.some((item: { id: string; platform_id: string }) => item.id === position.id && item.platform_id === "zhaopin")).toBe(true);
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
});

test("新用户需要终态筛选结果；扫描节点和分析中不算完成", async ({ request }) => {
  const user = await registerUser(request, "onboarding-result-contract");
  const headers = { authorization: `Bearer ${user.token}` };
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  const scanned = await request.post(`${CLOUD_URL}/api/user-flow`, { headers, data: { step: "first_resume_processed", source: "test_scan_count", status: "completed" } });
  expect(scanned.ok(), await scanned.text()).toBeTruthy();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  const analyzing = await request.post(`${CLOUD_URL}/api/user-flow`, { headers, data: { step: "first_screening_result", status: "completed", metadata: { kind: "ai", phase: "analyzing", terminal: false, accepted: false } } });
  expect(analyzing.status()).toBe(400);
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  const result = await request.post(`${CLOUD_URL}/api/user-flow`, { headers, data: { step: "first_screening_result", source: "local_agent", status: "completed", metadata: { kind: "ai", phase: "result", terminal: true, accepted: false } } });
  expect(result.ok(), await result.text()).toBeTruthy();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(true);
});

test("关键词判断不合适仍是有效跑通结果", async ({ request }) => {
  const user = await registerUser(request, "onboarding-keyword-contract");
  const result = await request.post(`${CLOUD_URL}/api/user-flow`, { headers: { authorization: `Bearer ${user.token}` }, data: { step: "first_screening_result", source: "local_agent", status: "completed", metadata: { kind: "keyword", phase: "result", terminal: true, accepted: false } } });
  expect(result.ok(), await result.text()).toBeTruthy();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(true);
});
