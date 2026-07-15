// 本文件负责集中管理各招聘平台打招呼后的追加点击策略。

/**
 * shouldClickGreetFollowups 判断平台完成首次打招呼后是否还需点击继续或确认按钮。
 * @param {string} platformID - 招聘平台标识。
 * @returns {boolean} 是否执行后续按钮点击。
 */
export function shouldClickGreetFollowups(platformID) {
  return String(platformID || "")
    .trim()
    .toLowerCase() !== "zhaopin";
}
