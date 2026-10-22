/** 文件作用说明：使用 IndexedDB 按团队与账号保存最多十个 AI 会话，无应用过期时间。 */
import { normalizeAssistantSessions, type AssistantSession } from "./assistant-protocol";

/** assistantAccountKey 同时包含团队与账号，禁止共享浏览器上跨团队读取历史。 */
export function assistantAccountKey(tenantID: string, userID: string): string {
  return tenantID.trim() && userID.trim() ? JSON.stringify([tenantID, userID]) : "";
}

/** openAssistantDB 打开本机缓存库；浏览器拒绝存储时明确报告，不假装已保存。 */
function openAssistantDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("goodhr5_assistant", 1);
    request.onupgradeneeded = () => { if (!request.result.objectStoreNames.contains("accounts")) request.result.createObjectStore("accounts"); };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(new Error("浏览器没有保存对话，允许本地存储后再试。"));
    request.onblocked = () => reject(new Error("对话缓存正在被其他页面占用，请关闭旧页面再试。"));
  });
}

/** readAssistantSessions 读取当前身份历史，损坏条目由结构校验剔除。 */
export async function readAssistantSessions(account: string): Promise<AssistantSession[]> {
  if (!account) return [];
  const db = await openAssistantDB();
  try {
    return await new Promise((resolve, reject) => {
      const transaction = db.transaction("accounts", "readonly"); const request = transaction.objectStore("accounts").get(account);
      request.onsuccess = () => resolve(normalizeAssistantSessions(request.result));
      request.onerror = () => reject(new Error("对话缓存没有读到，请重试。"));
    });
  } finally { db.close(); }
}

/** writeAssistantSessions 原子保存文字、工具记录与事件游标，保证刷新补发不重复。 */
export async function writeAssistantSessions(account: string, sessions: AssistantSession[]): Promise<void> {
  if (!account) return;
  const db = await openAssistantDB();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction("accounts", "readwrite"); transaction.objectStore("accounts").put(normalizeAssistantSessions(sessions), account);
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(new Error("对话没有存到浏览器，可能空间不足；当前页面仍可继续。"));
      transaction.onabort = () => reject(new Error("对话保存被中断，当前页面仍可继续。"));
    });
  } finally { db.close(); }
}
