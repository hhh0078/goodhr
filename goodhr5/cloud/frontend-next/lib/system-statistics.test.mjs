/** 文件作用：验证全站统计导出的金额精度、CSV公式防护、未知值和零分母，使用纯合成数据。 */
import test from "node:test";
import assert from "node:assert/strict";
import { shanghaiDate, statisticRate, statisticsCSV } from "./system-statistics.ts";

// 验证自然日以北京时间为准，和测试机本地时区无关。
test("统计默认日期采用北京时间", () => {
  assert.equal(shanghaiDate(new Date("2026-11-10T15:59:59Z")), "2026-11-10");
  assert.equal(shanghaiDate(new Date("2026-11-10T16:00:00Z")), "2026-11-11");
});

// 验证实际零、缺少样本和缺少数据三种情况不混淆。
test("漏斗比例区分零与未知", () => {
  assert.equal(statisticRate(0, 10), "0.0%");
  assert.equal(statisticRate(1, 3), "33.3%");
  assert.equal(statisticRate(0, 0), "—");
  assert.equal(statisticRate(undefined, 10), "—");
});

// 验证精确金额不经浮点数转换，模型名称不能在表格中执行公式。
test("导出精度、缺失值及公式安全", () => {
  const csv = statisticsCSV({
    start_date: "2026-11-10", end_date: "2026-11-10", as_of: "2026-11-10T04:00:00Z",
    amounts: { consumption: "90071992547409.1234", net_receipts: "-0.01" }, metrics: { registered: 0 },
    current: {}, current_amounts: {}, funnel: [], curve: [{ date: "2026-11-10", amount: "0.0001", calls: 1 }],
    platforms: [], sources: [], versions: [], errors: [], blocked: [], warnings: ["合成说明"],
    models: [{ name: '=HYPERLINK("https://example.invalid")', count: 1, amount: "0.0001" }],
  });
  assert.ok(csv.startsWith("\ufeff"));
  assert.ok(csv.includes('"90071992547409.1234"'));
  assert.ok(csv.includes('"-0.01"'));
  assert.ok(csv.includes("'=HYPERLINK"));
  assert.ok(csv.includes("暂无完整记录"));
  assert.ok(csv.includes('"新注册用户","0"'));
  assert.ok(!csv.includes("undefined"));
});
