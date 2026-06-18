# Prototype Instructions

Run the local server yourself and open the preview in the in-app browser. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

## GoodHR 新版前端设计约定

- 官网和后台将逐步迁移到 Next.js + MUI，旧 Vue 前端在迁移完成前继续保留。
- 视觉以明亮、简洁、留白充足为主，使用绿色品牌强调，不使用深色主题和蓝紫渐变。
- 首页参考 SeeLink 浅色排版结构，但不使用右侧产品卡片，首屏以单栏品牌信息和真实统计为主。
- 卡片圆角不超过 8px，避免大量卡片嵌套和无意义装饰。
- 官网公开页面使用 Next.js 服务端预渲染；公开统计只允许在服务端读取，浏览器不得直接请求云端统计接口。
- 任意官网地址中的 `invite` 参数由全局组件写入 `goodhr5_invite_id`，登录时统一提交，页面之间不得重复实现邀请参数逻辑。
- 新后台统一使用 `AdminApp`、`admin-api` 和 `AdminUI`；云端业务数据走云端 API，本地任务和浏览器操作只走 Local Agent。
- 后台采用左侧菜单、顶部状态栏、主内容区三块独立悬浮面板；菜单按工作台、招聘管理、团队与账户、本地与帮助、系统管理分组。
- 后台顶部状态栏不显示边框；未选中的侧栏文字和图标使用柔和中性色，避免与页面内容争抢视觉焦点。
- 后台输入框和按钮使用紧凑工作台尺寸；输入框与按钮同行时必须限制输入区域宽度，并保证按钮不被压缩或换字。
- 会员专属选择卡使用纯黑金配色，不使用渐变；普通功能卡继续使用当前主题色。
- 新增和编辑表单统一使用 `AdminDialog`，模式选择统一使用 `ChoiceCards`，系统 JSON 配置统一使用 `JsonEditor` 和 `JsonTree`。
- 后台默认使用松绿色主题，同时允许用户切换莓果红和琥珀色；选择结果保存在当前浏览器。
- 公开路由使用无扩展名地址，同时保留旧 `.html` 地址的永久重定向，避免损失已有搜索入口。
