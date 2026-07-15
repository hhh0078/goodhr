// 本文件负责验证动态列表发生增删和重排后仍能找到正确候选人。

import assert from "node:assert/strict";
import test from "node:test";

import {
  bestCandidateTextMatch,
  candidateCardLocator,
  candidateTextSimilarity,
} from "./candidate-match.js";

test("候选人定位器兼容缓存引用和扫描包装对象", () => {
  const directLocator = { locator() {}, innerText() {} };
  const wrappedLocator = { locator: directLocator, frameURL: "" };
  assert.equal(candidateCardLocator(directLocator), directLocator);
  assert.equal(candidateCardLocator(wrappedLocator), directLocator);
});

test("候选人列表插入其他卡片后仍按身份找到原候选人", () => {
  const actualTexts = [
    "黄女士 23岁 本科 健康顾问 映秀小学数学教师",
    "临时推荐卡片",
    "卿女士 本科 教育销售实习 2026年毕业",
  ];
  assert.deepEqual(
    bestCandidateTextMatch("卿女士", "卿女士 本科 教育销售实习 2026年毕业", actualTexts),
    { index: 2, score: 1 },
  );
});

test("同名候选人使用原始卡片文本区分", () => {
  const actualTexts = [
    "刘女士 本科 宋女士 工程管理 无教育经验",
    "刘女士 大专 课程顾问 三年教育销售经验",
  ];
  const match = bestCandidateTextMatch(
    "刘女士",
    "刘女士 大专 课程顾问 三年教育销售经验",
    actualTexts,
  );
  assert.equal(match?.index, 1);
});

test("完全不同的候选人文本不会被强行匹配", () => {
  assert.equal(
    bestCandidateTextMatch("卿女士", "卿女士 教育销售", ["黄女士 健康顾问"]),
    null,
  );
  assert.equal(candidateTextSimilarity("卿女士 教育销售", "黄女士 健康顾问") < 0.45, true);
});
