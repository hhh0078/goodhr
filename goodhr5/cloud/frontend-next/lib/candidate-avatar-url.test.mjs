/** 本文件负责验证简历页只加载 GoodHR 自托管候选人头像，不回连招聘平台图片。 */

import assert from "node:assert/strict";
import test from "node:test";
import { candidateAvatarAssetURL } from "./candidate-avatar-url.ts";

test("候选人头像只接受 GoodHR 自托管随机路径", () => {
  const path = `/api/public/candidate-avatars/${"a".repeat(64)}.png`;
  assert.equal(
    candidateAvatarAssetURL(path, "http://127.0.0.1:8084/"),
    `http://127.0.0.1:8084${path}`,
  );
  assert.equal(
    candidateAvatarAssetURL("https://img.example.com/avatar.png", "http://127.0.0.1:8084"),
    "",
  );
  assert.equal(
    candidateAvatarAssetURL("/api/public/candidate-avatars/short.png", "http://127.0.0.1:8084"),
    "",
  );
});
