/** 本文件负责把 GoodHR 自托管候选人头像路径安全转换为完整访问地址。 */

const CANDIDATE_AVATAR_PATH_PATTERN =
  /^\/api\/public\/candidate-avatars\/[0-9a-f]{64}\.png$/;

/** candidateAvatarAssetURL 只接受 GoodHR 生成的随机 PNG 路径，拒绝招聘平台远程图片。 */
export function candidateAvatarAssetURL(value: string, baseURL: string) {
  const path = String(value || "").trim();
  if (!CANDIDATE_AVATAR_PATH_PATTERN.test(path)) return "";
  return `${String(baseURL || "").replace(/\/+$/, "")}${path}`;
}
