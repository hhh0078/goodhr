// 文件作用：验证路由最外层异步异常会返回统一错误，不会导致 Worker 未处理拒绝退出。
import test from 'node:test';
import assert from 'node:assert/strict';
import { WorkerServer } from '../dist/http/server.js';

test('路由边界抛错后服务器仍能接收下一次请求', async () => {
  let calls = 0;
  const server = new WorkerServer('127.0.0.1', 0, {
    async handle(_request, response) {
      if (++calls === 1) throw new Error('boundary fixture');
      response.end('healthy');
    },
  });
  await server.start();
  try {
    const url = `http://127.0.0.1:${server.server.address().port}/`;
    const failed = await fetch(url);
    assert.equal(failed.status, 500);
    assert.equal((await failed.json()).error.code, 'INTERNAL_ERROR');
    assert.equal(await (await fetch(url)).text(), 'healthy');
  } finally {
    await server.stop();
  }
});
