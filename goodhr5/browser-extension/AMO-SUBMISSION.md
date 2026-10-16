<!-- 本文件说明 GoodHR Firefox 插件提交 Mozilla 签名时所需的信息、检查步骤和发布边界。 -->

# Firefox 插件 Mozilla 签名说明

## 提交方式

选择 Mozilla Add-ons 的 **Self-distribution / Unlisted**。正式版 Firefox 只能安装 Mozilla 签名后的 XPI，`dist` 中带 `unsigned` 标识的文件只用于本地检查，不能作为公开安装包。

提交前执行：

```text
npm run build:firefox:unsigned
npm test
npm run lint:firefox
```

提交文件为 `dist-firefox-unsigned/goodhr-resume-firefox-unsigned-1.0.10.zip`，也可以在只通过环境变量提供 `WEB_EXT_API_KEY` 和 `WEB_EXT_API_SECRET` 后执行 `npm run sign:firefox`。首次提交沿用固定扩展 ID `resume-assistant@goodhr.58it.cn`。如果 Mozilla 提示该版本已存在，必须同时提升 Chrome 和 Firefox 的统一版本号，不能只改单个平台。

## 1.0.10 本次更新

新增 BOSS 附件 PDF 文字层读取，新增精确域名 `https://www.zhipin.com/*`，保持顶层内容脚本和 `all_frames=false`。只递归可见同源 iframe，不读取隐藏 Vue、网站缓存、网络响应或内部变量，不修改招聘页面业务 DOM。插件在本机整理纯文本后，经 GoodHR 云端使用当前账号配置的 AI 识别基本资料，再由用户点击添加或更新触发完整解析与入库。两个阶段可能计费，纯文本可能包含姓名和手机号；失败不自动重发收费调用。固定扩展 ID 保持原样。此 ZIP 尚待 Mozilla 签名，不能冒充正式 Firefox 安装包。

公开发布配置可使用 `version_chrome` 和 `version_firefox` 分别记录已发布版本。Firefox 尚待 1.0.9 签名时继续保留已签名 1.0.7 下载和独立版本；Chrome 发布 1.0.9 不会要求现有 Firefox 用户安装尚未签名的更新。业务源码仍共用同一构建版本。

## 功能和审核说明

- 插件读取用户主动打开的猎聘企业端、猎聘猎头端、智联和前程无忧候选人详情，以及 BOSS 当前附件 PDF 的可见、已加载文字层。
- 插件不会自动点击招聘页面，不会购买、解锁或绕过手机号权限。
- 用户可以补充手机号、备注和推荐岗位，再主动将简历保存到自己的 GoodHR 账号。
- 插件会把候选人姓名、手机号、邮箱、地区、经历、头像、备注和推荐信息发送到 GoodHR，用于查重和简历库管理。
- BOSS 附件在本机去除 HTML，仅向 GoodHR 的固定附件解析接口发送纯文本。基本阶段仅返回姓名、手机号、性别及时间最早的工作/学校年月；完整阶段需用户点击保存。GoodHR 只调用该用户当前配置的 AI，未配置时提示，不自动更换服务。原文不回显到任务回执或磁盘草稿，确认保存时才作为简历正文入库。图片文字不支持 OCR，未加载的页会提示手动滚动后重读；总页数无法确认时需人工核对。
- 插件会读取当前候选人页面地址和可见内容，以判断支持的平台和提取简历。
- 登录令牌仅用于访问用户自己的 GoodHR 数据；没有广告、跨站追踪或第三方统计。
- 完整功能需要 GoodHR 测试账号。审核账号和密码只能填写在 AMO 的私密审核说明中，严禁写入仓库或安装包。

## 签名后发布门禁

1. 从 Mozilla 下载已签名 `.xpi`，不要用本地 ZIP 改名代替。
2. 使用正式 Firefox 安装并重启，确认插件仍存在。
3. 核对插件版本和扩展 ID，再计算文件大小与 SHA-256。
4. 上传 OSS 时使用不可覆盖的摘要文件名，并返回 `application/x-xpinstall`。
5. 从公网重新下载并复算 SHA-256，必须与本地签名文件一致。
6. 后台配置只在以上检查全部通过后填写 XPI 地址、摘要并设置 `firefox_signed=true`。
7. Chrome 地址和摘要必须保持不变。

Firefox 新版本签名或审核未完成时，继续提供已经验证的旧签名版；只有没有有效签名包时，公开接口和下载页才隐藏 Firefox 下载入口。
