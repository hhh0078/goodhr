export function deepClone(value) {
  return JSON.parse(JSON.stringify(value));
}
