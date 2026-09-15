// 文件作用说明：为同一个持久化 Profile 维护一份稳定的 Camoufox 指纹文件，保证同一账号的浏览器指纹不漂移。

import fs from "node:fs/promises";
import path from "node:path";
import { FingerprintGenerator } from "fingerprint-generator";
import type { Fingerprint } from "fingerprint-generator";

const FINGERPRINT_FILENAME = "goodhr-fingerprint.json";

const fingerprintGenerator = new FingerprintGenerator({
  browsers: ["firefox"],
  devices: ["desktop"],
  operatingSystems: ["windows", "macos"],
});

/** loadStableProfileFingerprint 读取 Profile 指纹文件；缺失或损坏时重新生成并写回，失败时不阻断启动。 */
export async function loadStableProfileFingerprint(
  userDataDir: string | undefined,
): Promise<Fingerprint | undefined> {
  const directory = userDataDir?.trim();
  if (!directory) {
    return undefined;
  }
  const filePath = path.join(directory, FINGERPRINT_FILENAME);
  const existing = await readFingerprintFile(filePath);
  if (existing) {
    return existing;
  }
  const generated = generateFingerprintSafely();
  if (!generated) {
    return undefined;
  }
  try {
    await fs.mkdir(directory, { recursive: true });
    await fs.writeFile(filePath, JSON.stringify(generated), "utf8");
  } catch {
    // 指纹文件写不进去时不阻断启动，浏览器会沿用本次生成的指纹。
  }
  return generated;
}

/** readFingerprintFile 读取并校验已有指纹文件，任何异常都按文件不存在处理。 */
async function readFingerprintFile(
  filePath: string,
): Promise<Fingerprint | undefined> {
  try {
    const content = await fs.readFile(filePath, "utf8");
    const parsed: unknown = JSON.parse(content);
    return isValidFingerprint(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

/** generateFingerprintSafely 生成 Firefox 桌面指纹，失败时返回 undefined 交给 Camoufox 随机生成。 */
function generateFingerprintSafely(): Fingerprint | undefined {
  try {
    return fingerprintGenerator.getFingerprint().fingerprint;
  } catch {
    return undefined;
  }
}

/** isValidFingerprint 校验指纹结构至少包含 Firefox UA、屏幕尺寸和显卡信息等关键字段。 */
function isValidFingerprint(value: unknown): value is Fingerprint {
  if (!value || typeof value !== "object") {
    return false;
  }
  const candidate = value as Partial<Fingerprint>;
  return (
    typeof candidate.navigator?.userAgent === "string" &&
    candidate.navigator.userAgent.includes("Firefox") &&
    typeof candidate.screen?.width === "number" &&
    typeof candidate.videoCard?.renderer === "string"
  );
}
