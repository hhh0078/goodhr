/**
 * 对字符串做简单哈希（djb2 算法），返回十六进制字符串
 * 用于公告内容去重判断，非密码学用途
 * @param str - 输入字符串
 * @returns 十六进制哈希值
 */
export function contentHash(str: string): string {
  let h = 5381;
  for (let i = 0; i < str.length; i++) {
    h = ((h << 5) + h + str.charCodeAt(i)) & 0xffffffff;
  }
  return (h >>> 0).toString(16);
}
