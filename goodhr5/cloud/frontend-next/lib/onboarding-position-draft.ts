/** 本文件仅在当前浏览器按账号保存新手创建岗位的六项核心草稿，不上传云端或保存账号数据。 */

const DRAFT_VERSION = 1;
const DRAFT_FIELDS = ["name", "platform_id", "mode_default", "detail_mode", "position_requirement", "keywords"] as const;

export type OnboardingPositionDraft = {
  name: string;
  platform_id: string;
  mode_default: string;
  detail_mode: string;
  position_requirement: string;
  keywords: string;
};

/** onboardingPositionDraftKey 将版本和当前账号加入本机缓存键，禁止不同账号共享草稿。 */
export function onboardingPositionDraftKey(identity: string): string {
  return `goodhr_onboarding_position_draft_v${DRAFT_VERSION}_${encodeURIComponent(identity.trim().toLowerCase())}`;
}

/** readOnboardingPositionDraft 安全读取当前账号的核心草稿，损坏、旧版或不可读缓存返回空。 */
export function readOnboardingPositionDraft(
  identity: string,
  storage?: Pick<Storage, "getItem">,
): OnboardingPositionDraft | null {
  if (!identity.trim()) return null;
  try {
    const cache = storage || (typeof window !== "undefined" ? window.localStorage : undefined);
    const value: unknown = JSON.parse(cache?.getItem(onboardingPositionDraftKey(identity)) || "null");
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const record = value as Record<string, unknown>;
    const draft = record.draft;
    if (record.version !== DRAFT_VERSION || !draft || typeof draft !== "object" || Array.isArray(draft)) return null;
    const fields = draft as Record<string, unknown>;
    if (DRAFT_FIELDS.some((field) => typeof fields[field] !== "string")) return null;
    if (!["boss", "zhaopin", "liepin", "hliepin"].includes(String(fields.platform_id))) return null;
    if (!["ai", "keyword"].includes(String(fields.mode_default))) return null;
    if (!["ai", "ocr", "dom"].includes(String(fields.detail_mode))) return null;
    return coreOnboardingPositionDraft(fields as OnboardingPositionDraft);
  } catch {
    return null;
  }
}

/** saveOnboardingPositionDraft 仅写入六项允许字段，完整表单和额外账号信息不会进入缓存。 */
export function saveOnboardingPositionDraft(
  identity: string,
  draft: OnboardingPositionDraft,
  storage?: Pick<Storage, "setItem">,
): boolean {
  if (!identity.trim()) return false;
  try {
    const cache = storage || (typeof window !== "undefined" ? window.localStorage : undefined);
    if (!cache) return false;
    cache.setItem(onboardingPositionDraftKey(identity), JSON.stringify({
      version: DRAFT_VERSION,
      draft: coreOnboardingPositionDraft(draft),
    }));
    return true;
  } catch {
    return false;
  }
}

/** clearOnboardingPositionDraft 在岗位真实创建成功后只清除当前账号草稿，其他账号保持原样。 */
export function clearOnboardingPositionDraft(
  identity: string,
  storage?: Pick<Storage, "removeItem">,
): boolean {
  if (!identity.trim()) return false;
  try {
    const cache = storage || (typeof window !== "undefined" ? window.localStorage : undefined);
    if (!cache) return false;
    cache.removeItem(onboardingPositionDraftKey(identity));
    return true;
  } catch {
    return false;
  }
}

/** coreOnboardingPositionDraft 显式复制允许保存的字段，避免新表单字段被意外持久化。 */
function coreOnboardingPositionDraft(draft: OnboardingPositionDraft): OnboardingPositionDraft {
  return {
    name: draft.name,
    platform_id: draft.platform_id,
    mode_default: draft.mode_default,
    detail_mode: draft.detail_mode,
    position_requirement: draft.position_requirement,
    keywords: draft.keywords,
  };
}
