/**
 * 深拷贝工具函数
 * @param value - 需要深拷贝的值
 * @returns 深拷贝后的值
 */
export function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value));
}
