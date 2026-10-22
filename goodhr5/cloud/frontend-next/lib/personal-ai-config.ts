/** 本文件负责个人 AI 配置的域名判断、内置模型整理和价格显示。 */

/** BuiltinAIModel 表示个人配置页可选的内置模型及每百万 Token 的价格。 */
export type BuiltinAIModel = {
  id: string;
  name: string;
  description: string;
  input_price_per_1m_cents?: number;
  output_price_per_1m_cents?: number;
};

/** usesBuiltinAIModels 严格核对 HTTP(S) 地址的真实主机，拒绝路径伪装、相似域名和用户信息。 */
export function usesBuiltinAIModels(baseURL: string, hostname: string) {
  const domain = hostname.trim().toLowerCase();
  if (!domain) return false;
  try {
    const url = new URL(baseURL.trim());
    return ["http:", "https:"].includes(url.protocol) && !url.username && !url.password && url.hostname.toLowerCase() === domain;
  } catch { return false; }
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

/** resolvePersonalAIModel 同域接口的无效模型回到首项，外部接口和未读取的列表保留原值。 */
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
