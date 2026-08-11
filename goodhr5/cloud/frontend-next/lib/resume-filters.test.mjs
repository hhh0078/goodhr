/** 本文件负责验证简历库地址筛选的默认值、白名单和查询生成。 */

import assert from "node:assert/strict";
import test from "node:test";
import {
  resumeFiltersFromParams,
  resumeListQuery,
  resumePageNumber,
} from "./resume-filters.ts";

test("缺少筛选参数时使用产品默认值", () => {
  const filters = resumeFiltersFromParams(new URLSearchParams());
  assert.equal(filters.platformID, "");
  assert.equal(filters.phoneStatus, "has");
  assert.equal(filters.conditionStatus, "all_matched");
  assert.equal(filters.sort, "second_score_desc");
});

test("合法筛选忽略首尾空格和大小写", () => {
  const filters = resumeFiltersFromParams(
    new URLSearchParams({
      platform_id: " ZHAOPIN ",
      has_phone: " NONE ",
      condition_status: " Pending ",
      sort: " RECENT ",
    }),
  );
  assert.equal(filters.platformID, "zhaopin");
  assert.equal(filters.phoneStatus, "none");
  assert.equal(filters.conditionStatus, "pending");
  assert.equal(filters.sort, "recent");
});

test("非法筛选回退到与后端一致的全部或最近入库", () => {
  const filters = resumeFiltersFromParams(
    new URLSearchParams({
      platform_id: "unknown",
      has_phone: "unknown",
      condition_status: "unknown",
      sort: "unknown",
    }),
  );
  assert.equal(filters.platformID, "");
  assert.equal(filters.phoneStatus, "all");
  assert.equal(filters.conditionStatus, "all");
  assert.equal(filters.sort, "recent");
});

test("查询参数保留有效筛选并省略空条件", () => {
  const query = resumeListQuery(
    {
      keyword: "  数学老师  ",
      positionID: "position-1",
      platformID: "liepin",
      phoneStatus: "has",
      conditionStatus: "all_matched",
      sort: "second_score_desc",
    },
    2,
    20,
  );
  assert.equal(query.get("q"), "数学老师");
  assert.equal(query.get("position_id"), "position-1");
  assert.equal(query.get("platform_id"), "liepin");
  assert.equal(query.get("page"), "2");
  assert.equal(query.get("page_size"), "20");
});

test("页码只接受正整数并限制最大分页大小", () => {
  assert.equal(resumePageNumber("2", 1), 2);
  assert.equal(resumePageNumber("0", 1), 1);
  assert.equal(resumePageNumber("bad", 10), 10);
  assert.equal(resumePageNumber("999", 10, 100), 100);
});
