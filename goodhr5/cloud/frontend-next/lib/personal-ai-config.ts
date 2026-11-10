/** 本文件负责个人 AI 配置的域名判断、内置模型整理和价格显示。 */

/** BuiltinAIModel 表示个人配置页可选的内置模型及每百万 Token 的价格。 */
export type BuiltinAIModel = {
  id: string;
  name: string;
  description: string;
  input_price_per_1m_cents?: number;
  output_price_per_1m_cents?: number;
};

/** isOfficialAIURL 核对真实主机是否属于58it.cn，拒绝路径、查询字符串、相似域名与用户信息伪装。 */
export function isOfficialAIURL(address: string): boolean {
  try {
    const endpoint = new URL(address.trim());
    const host = endpoint.hostname.toLowerCase();
    return ["http:", "https:"].includes(endpoint.protocol) && !endpoint.username && !endpoint.password && (host === "58it.cn" || host.endsWith(".58it.cn"));
  } catch { return false; }
}

/** usesBuiltinAIModels 使用官方域名策略选择模型列表，保留旧主机参数供调用方兼容。 */
export function usesBuiltinAIModels(baseURL: string, _hostname: string) {
  return isOfficialAIURL(baseURL);
}

/** normalizeBuiltinAIModels 保留有效且不重复的模型，按系统配置原顺序排列。 */
export function normalizeBuiltinAIModels(source: unknown): BuiltinAIModel[] {
  if (!Array.isArray(source)) return [];
  const ids = new Set<string>();
  const models: BuiltinAIModel[] = [];
  for (const item of source) {
    if (!item || typeof item !== "object") continue;
    const id = typeof item.id === "string" ? item.id.trim() : "";
    if (!id || ids.has(id)) continue;
    ids.add(id);
    models.push({
      id,
      name: typeof item.name === "string" ? item.name.trim() || id : id,
      description:
        typeof item.description === "string" ? item.description.trim() : "",
      input_price_per_1m_cents: validModelPrice(item.input_price_per_1m_cents),
      output_price_per_1m_cents: validModelPrice(item.output_price_per_1m_cents),
    });
  }
  return models;
}

/** resolvePersonalAIModel 官方接口的无效模型回到首项，外部旧配置留待用户主动确认切换。 */
export function resolvePersonalAIModel(
  baseURL: string,
  model: string,
  hostname: string,
  models: BuiltinAIModel[] | null,
) {
  if (!usesBuiltinAIModels(baseURL, hostname) || !models?.length) return model;
  const value = model.trim();
  return models.some((item) => item.id === value) ? value : models[0].id;
}

/** formatBuiltinAIPrice 将分转换成元，缺失价格明确显示为未配置。 */
export function formatBuiltinAIPrice(cents: number | undefined) {
  const price = validModelPrice(cents);
  return price === undefined ? "未配置" : `¥${(price / 100).toFixed(2)}`;
}

/** validModelPrice 接受非负有限数字，避免缺失值或错误值被误显示成免费。 */
function validModelPrice(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : undefined;
}

/** normalizeAIBaseURL 补全 OpenAI 兼容的 Chat Completions 地址。 */
export function normalizeAIBaseURL(baseURL: string) {
  const value = baseURL.trim().replace(/\/+$/, "");
  if (!value) return "";
  if (value.endsWith("/chat/completions")) return value;
  if (value.endsWith("/v1")) return `${value}/chat/completions`;
  return `${value}/v1/chat/completions`;
}
