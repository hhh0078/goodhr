<!-- 文件作用：记录简历标签、岗位开关、猎头推荐入口和浏览器插件更新的范围、验证及生产回退证据。 -->

# 简历标签、岗位状态和猎头推荐更新（2026-10-15）

状态：本地验证完成，正式发布演练进行中。

## 实现范围

- 简历操作固定竖排为备注、推荐、面试；面试暂不执行请求。整行双击打开详情，原编辑入口保留在详情内。
- 岗位/推荐区域不再显示记录数。表格上方提供紧凑、可横向滚动的标签栏，显示当前账号可查看的真实候选人数。
- 标签区分团队和个人；个人标签即使面对团队管理员也只对创建者可见。最近点击顺序按账号保存在浏览器缓存。
- 标签管理支持新增、改名、范围调整和删除。管理员不能把他人团队标签改为个人标签；删除标签只删除分类关联。
- 原自由文本标签回填为简历创建者个人标签，候选人原行保持不变。携带标签加入团队、重复候选人合并也保持分类关联。
- 候选人维护选择已有标签；未知名称失焦后确认创建，默认个人范围。插件仅可搜索选择已有标签，不能创建标签。
- 岗位 `is_open` 独立于运行状态，旧岗位默认开启。关闭不删除岗位、简历或推荐历史，禁止新推荐和新任务。
- 岗位管理默认显示开启岗位，可切换关闭列表；推荐岗位选项由后端强制返回开启岗位，写入时再次检查。
- 菜单新增猎头推荐。参考 `D:/work/headhunter-system` 的 `himatch` 分支推荐记录页面，复用 GoodHR 原推荐阶段、备注和历史，提供分页搜索、新增、管理和删除。
- 推荐列表包含关闭岗位历史；删除推荐只删除该关系和跟进事件，保留候选人及岗位。

## 数据与接口

新增迁移 `0094_candidate_tags.sql` 和 `0095_position_open.sql`，共新增两张标签表和岗位开关列，不新增推荐流程表。

- `GET/POST /api/candidates/tags`；`PATCH/DELETE /api/candidates/tags/{id}`。
- 候选人保存使用 `tag_ids`；标签过滤使用 `tag_id`。
- `GET /api/positions?is_open=true|false|all`；推荐选项使用 `purpose=recommendation`。
- `PATCH /api/positions/{id}` 可只保存开启状态。
- `GET /api/candidate-recommendations` 服务端分页筛选；`DELETE /api/candidate-recommendations/{id}` 安全删除推荐。

## 浏览器发布边界

Chrome 1.0.8 正式发布；Firefox 1.0.8 提供未签名 ZIP 供用户自行上传 Mozilla。原 Firefox 1.0.7 签名包保留。

公开接口支持按浏览器返回独立版本，避免现有 Firefox 被要求下载尚未签名的新版本。旧本地 Agent 无浏览器参数的请求使用临时 `agent_compatibility` 快照，仍取得匹配版本的旧安装包。Firefox 1.0.8 完成签名且与 Chrome 版本一致后，可同时更新 Firefox 版本/地址/摘要并移除该快照。

## 已完成验证

- 后端全量测试和静态检查通过。
- 前端 36 项单元测试、TypeScript 检查通过；标签、岗位、推荐和下载页面浏览器回归通过。
- 插件 95 项测试、UI 冒烟及 Chrome 155 临时真实加载通过；Firefox lint 无错误，保留已有 Android 数据授权兼容警告。
- 独立 PostgreSQL 10 项简历/标签/岗位/推荐集成测试及团队邀请迁移测试通过。
- 正式迁移须完整数据库备份、隔离恢复及全部旧表数据指纹比较后执行。

## 面板备份保留

磁盘占满来源是宝塔内置每日面板 ZIP 备份，原逻辑固定保留 15 天。用户清理历史文件后，新增独立清理脚本，每 15 分钟只检查 `/www/backup/panel/YYYY-MM-DD.zip`，保留最近三份；不修改宝塔源码、不处理 GoodHR 发布备份。

脚本：`/opt/goodhr-maintenance/panel_backup_retention.py`。
原计划任务备份：`/opt/goodhr-maintenance/crontab-before-panel-retention-20261015.txt`。

## 正式发布与回退

本轮发布目录固定为 `/opt/goodhr-release/resume-tags-20261015`。完整数据库备份、旧容器、迁移核对材料和配置前后快照都保留。应用回退不删除新表或新资料，插件配置使用整行并发检查后单独回退。

```sh
python3 /opt/goodhr-release/resume-tags-20261015/resume-tags-20261015.py rollback
python3 /opt/goodhr-release/resume-tags-20261015/resume-tags-20261015.py rollback-config
```

正式发布结果待验收后补充。
