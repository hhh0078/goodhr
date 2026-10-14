<!-- 文件作用：记录HiMatch参考插件逐功能源码核对结果、GoodHR实现边界及1.0.3验收范围。 -->
# HiMatch插件功能核对与GoodHR 1.0.3

参考目录：`D:\work\headhunter-system-goodhr-reference\browser-extension`，himatch分支，清单版本0.5.2。已完整读取background、content、popup、全部liepin/zhaopin/51job适配器及manifest。该目录没有独立options页面。以下“参考有”指源代码确实存在，不代表运行过真实账号。

| 功能 | 参考源码证据（相对browser-extension） | GoodHR 1.0.3处理 |
| --- | --- | --- |
| 候选人识别后自动查重 | content/content.js:801 boot、932 pollPageState | 登录且同意后自动查询；捕获450ms防抖，UI补号/姓名变化重查 |
| 有手机号精确查询 | background/background.js:214 handleCandidateDuplicate | 手机号或平台ID精确匹配；只唯一exact自动选 |
| 无号综合年龄/最早学校公司查询 | content/content.js:390—482 criteria，background:214 | 发送gender/age/birth_ym、学校公司开始年月；展示服务端实际criteria与groups |
| 兜底学校/公司分组 | content/content.js:368 buildDuplicateGroupsHTML | 展示分组依据；possible即使一条也必须人工选 |
| 已入库/未入库/无手机号/失败提示 | content/content.js:801 boot | 明确状态；查重失败保留输入并允许重试，隐藏资料不露信息 |
| 同时多个人或迟到查询 | 参考默认取首条candidateId | 不自动选多条，账号+候选人+查询序号检查后再显示 |
| 缺号补填 | content/content.js:661 importCandidate使用prompt | 表单手机号常驻，掩码/多号留空；大陆及明确+国际号 |
| 已有无号记录备注 | content/content.js:327 addCandidateNote | 无号仍可独立备注/推荐；新增与资料更新要求完整号 |
| 主体创建/更新 | background/background.js:338/406 | 扁平snake_case，CREATE幂等，PATCH行版本和空值保护 |
| 更新不隐式保存备注 | content/content.js:719 updateCandidate | 主体更新不调用备注/推荐；输入继续保留 |
| 新建之后备注与岗位分别保存 | content/content.js:661 importCandidate | 先取得candidate.id，再独立提交；各自失败沿用幂等键重试 |
| 独立备注按钮 | content/content.js:327/531 | 明确“加备注”，不需要再次更新主体 |
| 备注作者、发生时间、换行 | content/content.js:282 buildCandidateNotesHTML | 独立历史，occurred_at优先、created_at备用；可填发生时间 |
| 备注特殊禁止值1 | content/content.js:337、background备注校验 | 不照搬无业务依据的规则，遵循GoodHR1000 Unicode字符上限 |
| 岗位搜索 | content/content.js:492/513 | 完整岗位列表，名称/公司/城市筛选，保留已选岗位 |
| 岗位只取20个 | background/background.js:354 pageSize=20 | GoodHR全部岗位，不保留这个限制 |
| 推荐状态 | background/background.js:361只取第一个pipeline节点 | 选择draft/recommended/contacting/interview/offer/hired/paused，默认已关联 |
| 推荐专属备注/发生时间/事件 | 参考CREATE_RECOMMENDATION固定备注、无事件界面 | 独立推荐内容和occurred_at；展示from_stage→to_stage、操作人、时间 |
| 完整简历详情入口 | content/content.js:129/256 | 精确链接GoodHR候选人详情，不传招聘查询串 |
| 头像采集与本地化 | background/background.js:283 normalizeCandidatePayload；三平台头像选择器 | 仅可见头像；点击保存后有限域无Cookie读取，256px私有dataURL；失败保留原头像并独立重试 |
| 平台结构化经历字段 | platforms/liepin.js、zhaopin.js、51job.js完整extractResume | 纯读选择器核对并规范snake_case/年月；多期望职位城市、智联技能等级与明确证书 |
| 猎聘证书/荣誉、51job证书/荣誉 | 参考没有对应extract函数 | 留空、不猜选择器；主简历库可维护 |
| 猎聘企业lpt与嵌套预览 | 参考manifest仅h.liepin.com | GoodHR另用当前主仓库nest读取依据，覆盖chat预览和独立resume/detail |
| 只读安全 | 智联revealUnlockedPhone写dataset并button.click；猎聘phone兜底body | 不复制自动解锁、整页电话猜测、隐藏节点或第三方iframe读取；仅允许自己的隔离UI容器 |
| 猎聘猎头手机号行内匹配 | platforms/liepin.js:133—153 contactRows/normalizePhone/extractPhone | 当前候选人 .connect-box dd 内识别号码旁复制等文字、可见电话链接；排除邮箱、编号、掩码、隐藏和多号，不复制body兜底 |
| 登录状态及角色 | popup/popup.js:3/66，background:165 | 插件受限会话，后台持凭证；邮箱密码/验证码及本地配对，不交给content/UI token |
| 优先本地后云端登录 | background/background.js:165 | GoodHR固定云认证；已登录本地通过明确配对授权恢复 |
| 退出 | 参考只删除浏览器token | 云会话/本地grant分别撤销，失败私有保留待撤销密钥并明确重试 |
| 手动登录后配对 | 参考没有独立授权桥 | 同账号可配对并撤销旧手动会话；不同本地账号的新授权撤销，先退出再连接 |
| 版本更新 | background/background.js:103任何版本不同都锁住 | 公开system.browser_extension，仅较新版本提示官方下载；开发新版本不会被旧配置锁死 |
| AI流式思考与正文 | background/background.js:424云端stream；content:741 | 本地配对任务开始/轮询/取消/显式重试；不复制云端AI或思考流，只显示职业摘要和任务状态 |
| AI保存 | content/content.js:741更新整个candidate | 总结先存草稿，明确单独PATCH ai_summary+row_version；不隐式改其它资料 |
| AI切人隔离 | content/content.js:232 resumeKey | 摘要任务与草稿按人和账号隔离；generation拒绝旧账号结果 |
| 关闭后的草稿 | 参考只保留content内存中的note draft | 本地24小时内有效、最多30份/3MiB；冻结职业资料≤1MiB，并发串行写入，存储失败明确提示 |
| 浮层/位置/拖拽 | content/content.js:153—223/531 | 新授权下创建正常命名的closed Shadow宿主，点击加载扩展域iframe，自己的标题栏拖动/收起且限窗口边界；不改招聘业务DOM，不做随机标识检测规避；刷新后位置回到右下角 |
| Alt+Enter | content/content.js:996调用招聘页浮层按钮click | 仅插件自身UI快捷键；不在招聘页安装键盘监听 |
| Chrome/Firefox | 参考Chrome MV3 service_worker | 共用1.0.2，Chrome service_worker+side_panel，Firefox background scripts+sidebar_action |
| options配置 | 参考无独立options文件 | 本地端口与配对在自身UI；业务域名固定白名单，无任意代理 |

## 字段合同与外发范围

候选人资料使用当前云端白名单。1.0.8 起库标签通过 tag_ids 选择当前账号可见的已有标签，最多30个；采集 tags 技能文字保留本地预览和总结，不提交云端创建标签。current_region/expected_region为地区，数值薪资仅明确的expected_salary_min/max，单位K，原文字另保留current_salary_text/expected_salary_text；日期YYYY-MM。经历使用work_experiences、educations、project_experiences、certificates、honors。头像只允许规范化PNG/JPEG完整dataURL≤131072字符。UPDATE过滤空采集字段、空经历数组和null，保留主库资料；显式 tag_ids:[] 仅清除当前用户可见的标签关联。

查重发送有限身份依据，不发送完整履历和头像。AI通过配对grant调用127.0.0.1的本地任务接口，移除姓名、联系方式字段、头像和来源编号，再由本地程序按配置调用AI服务，职业事实会交给该服务处理。没有云端执行AI或完整网页登录Token读取回退。失败不自动重新生成；possible charge显示并要求显式重试。

摘要completed后显式生成用新请求键；failed/cancelled同资料只可明确重试原任务，职业事实已变可以确认后新建。首次响应未知冻结去联系后的source和旧请求键，仅恢复上次任务；不因改职位而误重发AI。选择岗位默认draft/已关联，明确选择其它状态才推进。

## 验证范围与限制

Node测试包含四平台可见范围、合成嵌套/独立DOM、字段合同、空值更新保护、头像有限域及尺寸、鉴权/撤销/异步竞态、结构化查重、独立备注推荐时间、草稿与本地摘要。Chrome测试在本地合成DOM和插件自身UI运行，所有外部网络阻断；覆盖自动相似/唯一精确选择、45岗位、独立重试、无号旧记录、关闭/切人/切号草稿、迟到查询、摘要和头像转换。

1.0.3新增号码旁复制文案、联系方式邮箱/年份/编号/掩码与多号拒绝，closedShadow样式隔离、懒加载、拖动边界、收起保留输入、网页消息无效；后台验证iframe自身tab与当前capture_id，头像读取期间切人不写库。内置示例标记与预留ID在AI职业资料整理之前拒绝，不按姓名/手机号误伤真实资料。Chrome154独立临时配置实际加载扩展后，合成猎聘详情iframe的sender.tab与frameId证据确认有效；不依赖activeTab读取嵌入来源，也不引入额外网页nonce。

尚未宣称真实招聘账号的全部页面通过；企业头像选择器及前程无忧额外CDN仍待真实页面核实。参考项目没有猎聘/51job证书荣誉读取，未凭猜测新增。本轮仅交付Chrome 1.0.3开发包，尚未发布Chrome商店；Firefox1.0.2历史ZIP/XPI未签名，仅临时加载，新版分发暂缓。招聘页CSP可能阻止嵌入扩展iframe，此时工具栏/原生侧栏仍可用。悬浮实现复用现有完整界面，不照搬网页凭证、业务DOM修改、云端AI或自动解锁。
