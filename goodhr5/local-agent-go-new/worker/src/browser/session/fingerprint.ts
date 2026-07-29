// 文件作用说明：为同一个持久化 Profile 生成稳定的 CloakBrowser 指纹种子参数。

import { createHash } from "node:crypto";

/** withStableProfileFingerprint 在调用方未指定指纹时，为持久化 Profile 补充稳定种子。 */
export function withStableProfileFingerprint(
  args: readonly string[] | undefined,
  userDataDir: string | undefined,
): string[] {
  const result = [...(args ?? [])];
  if (
    !userDataDir ||
    result.some(
      (argument) =>
        argument === "--fingerprint" ||
        argument.startsWith("--fingerprint="),
    )
  ) {
    return result;
  }
  const digest = createHash("sha256").update(userDataDir).digest();
  const seed = 10_000 + digest.readUInt32BE(0) % 90_000;
  return [...result, `--fingerprint=${seed}`];
}
