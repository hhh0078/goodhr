# Edge 92 浏览器兼容

本文说明云端 Next.js 前端的浏览器目标、兼容补丁边界与验证方式。

## 原因与处理

2026-11-16 用户反馈 Edge 92.0.902.67 打开登录和后台时报 `Unexpected token '{'`。
从公开站点读取报错的 `0ui2vnxn8sjmb.js`，其中 Next.js 客户端组件包含类静态初始化块。
将该脚本交给 Chromium 92.0.4515.0 真实编译，可复现同样错误。

- `cloud/frontend-next/package.json` 显式设置 Chrome/Edge 92 起的编译目标，Firefox 111 和 Safari 16.4 的原有最低目标保持不变，同时包含各浏览器更新版本。
- `instrumentation-client.ts` 在客户端组件初始化前同步加载 `lib/browser-polyfills.ts`，不通过异步下载与页面初始化竞争。
- 按需补齐 `Array.at/findLast/findLastIndex`、`Object.hasOwn` 和 `Promise.withResolvers`。使用固定版本 core-js，不改写页面业务逻辑。
- `crypto.randomUUID` 缺失时只使用浏览器 `getRandomValues` 生成第四版 UUID；保留原生方法，不使用 `Math.random`。缺少安全随机数时不伪造结果。
- 补丁属于网站客户端，不进入 Go、本地 Agent、Worker 或招聘网站，不放宽招聘页面零注入规则。

## 验证

正式构建命令为前端目录中的 `npm run build`；单元测试为 `npm test`。
`lib/browser-polyfills.test.mjs` 另可单独运行：

```bash
node --experimental-strip-types --test lib/browser-polyfills.test.mjs
```

本轮检查使用正式构建与合成接口，未向真实账号写入资料、启动任务或发起支付：

- TypeScript 和正式构建通过，179 项单元测试通过。
- Chromium 92 实际编译全部 91 个发布脚本，无语法错误；同一内核编译原线上报错脚本可复现错误。
- Chromium 92.0.4515.0、Chrome 157.0.8103.51、Firefox 150.0.2 检查 1366px 与 390px 下的登录，以及岗位、简历、推荐、企业招聘、个人配置、订阅和系统配置页面。
- 同时检查新建岗位弹框打开/取消、简历备注输入与分页；备注输入会执行安全 UUID 补丁，未提交真实备注。
- 单元测试分别模拟旧浏览器缺少方法和新版浏览器已有方法，确认标准方法行为、空数组与负索引、UUID 格式、重复初始化，以及原生方法身份保持。

本轮旧版实测对象是 macOS 上的 Chromium 92，同主版本内核验证不能代替用户 Windows 上的 Edge 92.0.902.67 原机验收；未声称所有后台业务、Safari 真机或本地程序操作已经完整验证。发布后的浏览器回归仍需使用本轮真实镜像与公开入口确认。
