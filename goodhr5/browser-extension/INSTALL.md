<!-- 文件作用：介绍 GoodHR 简历插件的开发包安装、只读采集、登录和失败重试方式。 -->
# GoodHR 简历助手 1.0.1

本包是开发测试包。Chrome ZIP 可解压加载；Firefox ZIP / `unsigned.xpi` 只能临时加载，**没有商店审核或 Mozilla 签名**，不能当作正式安装包。

## Chrome 安装

1. 解压 `goodhr-resume-chrome-1.0.1.zip`。
2. 打开 `chrome://extensions`，打开“开发者模式”。
3. 点“加载已解压的扩展程序”，选择含 `manifest.json` 的解压目录。
4. 将 GoodHR 图标固定在浏览器工具栏。需要 Chrome 116 或更高版本。

## Firefox 临时安装

1. 解压 `goodhr-resume-firefox-1.0.1.zip`。
2. 打开 `about:debugging#/runtime/this-firefox`。
3. 点“临时载入附加组件”，选择解压目录的 `manifest.json`，或直接选择 `goodhr-resume-firefox-1.0.1-unsigned.xpi`。
4. Firefox 重启后临时插件会移除，重新加载即可。需要 Firefox 140 或更高版本。

正式 Firefox 安装需单独完成 Mozilla 签名。Chrome 正式公开分发需独立的商店流程。本版本不自动安装到 Agent / Camoufox，也不改变原来的招聘自动化。

## 登录和使用

1. 点击 GoodHR 图标，未登录也可查看当前已读取的姓名、手机号和中文经历预览。“重新读取”仅在本地采集，不请求云端；要查重和保存时，再同意将确认后的候选人资料保存到 GoodHR。
2. 用 GoodHR 邮箱密码或验证码登录。密码不会保存在插件。也可选择“本地程序已经登录？”：输入本地端口（默认 43129）并连接，在打开的 GoodHR 控制台确认一次。后续同一安装可以自动恢复已配对账号。
3. 打开猎聘企业端 `lpt.liepin.com`（含聊天页 `/chat/im#preview` 的弹框和独立页 `/resume/detail`）、猎聘猎头端 `h.liepin.com`、智联 `rd6.zhaopin.com`、前程无忧 `ehire.51job.com` 的候选人详情。可见详情被识别后，工具栏出现 `+`；点击图标或“放到侧栏”继续操作。
4. 招聘页面没有悬浮框。插件只读可见 DOM，不改页面、不自动点击、不滚动、不解锁联系方式、不购买手机号、不读招聘 Cookie、不拦截网络。
5. 请检查姓名和手机。未识别到手机号、掩码号码或不完整号码都需手动补齐。本期支持完整中国大陆 11 位手机号，可带 `+86` 前缀。
6. 点“查重”，手机号/平台 ID 精确结果与姓名/经历等相似线索分别提示。“可能重复”需要你自己确认，插件不会自动合并或覆盖。
7. 新人点击“添加简历”；已有候选人先选择对应结果，可单独“保存备注 / 关联岗位”。更新已有资料先显示具体变化，再明确确认并带版本校验。未读到的字段、空值和空经历均保留已有内容；若要主动清空字段，请到 GoodHR 简历库手动编辑。选岗位只新建“已关联”记录，不自动推进推荐状态。
8. 备注和岗位分别保存，记录可在插件内查看。某一项失败时输入会保留，点保存重试即可。重试沿用对应幂等键，不重复创建已成功的简历。
9. 识别到头像时会提示“保存时一并读取”，未登录预览不请求第三方图片。点击保存后后台只从当前平台及猎聘 `*.lietou-static.com`、智联 `mypics.zhaopin.cn` 的有限图片域读取，拒绝重定向，不发送网站 Cookie。图片原文最多 1 MiB、最多 800 万像素，整理为最长边256像素的 PNG/JPEG，完整内嵌数据最多131072字符。WebP 在插件后台转换。头像失败不会清空已有头像，可单独重试，不重复创建简历或备注；保存成功显示 GoodHR 已存头像。

关闭弹窗会丢弃尚未保存的表单内容；较长备注建议先打开侧栏。退出会停止使用账号，并分别撤销云端会话和本地连接授权。本地程序离线或网络失败时，界面会明确显示“仍未撤销”，私有保存待撤销凭证，禁止自动恢复；重新运行本地程序后点“重试撤销旧授权”。后台在界面打开时也会间隔重试。撤销完成后连接本地账号需要重新授权。卸载前请先完成撤销，避免本地授权仍留在本地程序中。暂不支持第三方页面 iframe 内详情；多位详情同时可见时不猜测，收起多余详情后重新读取。

## 采集范围和资料说明

仅读取当前可见候选人的姓名、完整手机号、邮箱、年龄/性别、城市/学历/职位/求职状态和可见经历。页面未显示的字段留空，薪资保留原文字，避免把年薪误当月薪。原始页面、截图、网站 Cookie 和隐藏节点不上传。

各平台会调整页面结构。当前适配依据仓库已有选择器和参考项目源码；新页面或缺字段时可人工补齐并到简历库编辑。不代表已用真实招聘账号完成全部页面验收。未发现候选人时可刷新招聘页面、检查是否已授予该站点访问权限，再点击“重新读取”。

企业端头像只使用明确候选人区域兼容选择器，仍待真实页面核实；前程无忧目前仅同域图片，参考清单未发现额外图片 CDN 证据。图片域不支持或头像未识别时，可先保存其它资料，稍后重新读取后重试。参考猎聘插件本身没有证书/荣誉解析，本插件不猜测这些选择器；智联保留已有明确证书读取。已存在候选人可点击“查看完整简历”打开 GoodHR 详情。

本地程序尚未升级时，首次配对接口不可用，请使用手动登录。网络失败请检查当前连接；手机号或版本冲突请重新查重。不要为插件授予所有站点权限。

插件登录提示 404 时，表示当前 GoodHR 服务还没有更新插件登录接口，可先用未登录的只读预览，服务更新后再登录。插件不会伪造登录或借用网页登录凭证。

## 本地构建与测试

无需安装依赖：在 `goodhr5/browser-extension` 运行 `npm test`、`npm run build`。构建输出在 `dist/chrome`、`dist/firefox` 和三个开发压缩包；版本只取 `package.json`，与本地 Agent 版本相互独立。`dist/checksums.json` 记录 SHA256。

`node scripts/export-create-fixture.js` 会从合成输入通过实际插件整理器导出 `tests/fixtures/plugin-candidate-create.json`，供云端 Go 路由测试直接验证。标签为文字数组，地区使用 `current_region` / `expected_region`；明确数值薪资单位为 K，页面原文字单独保存。该文件和企业端嵌套预览 fixture 均不包含真实个人资料。

已有仓库前端 Playwright 和本机 Chrome 时，可运行 `npm run test:ui` 做插件本地界面假接口验证。该验证不访问招聘网站、不登录真实账号，截图写到忽略目录 `artifacts`。缺浏览器时不自动下载，请使用已经安装的 Chrome。

官方说明：[Chrome 侧栏](https://developer.chrome.com/docs/extensions/reference/api/sidePanel)、[Firefox 后台清单](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/manifest.json/background)、[Firefox 数据授权](https://extensionworkshop.com/documentation/develop/firefox-builtin-data-consent/)、[Mozilla 签名分发](https://extensionworkshop.com/documentation/publish/signing-and-distribution-overview/)。
