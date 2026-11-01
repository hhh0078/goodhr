<!-- 文件作用：记录岗位平台编辑、人才页面适配的真实云端发布身份、Mac可选更新及隔离/回退证据，不保存凭据或候选人资料。 -->

# 平台人才页面正式发布记录（2026-11-02）

## 实际上线身份

- 云端部署源为 `2176fac5e93e4c138cefdbddea454fee15d9938c`，发布标签 `platforms-2176fac5-20261102`。
- 本文是发布后补记；后续文档提交或 main/dev 合并提交均不是此次运行镜像的源码身份。
- 发布基于原生产 `cabe422e83394f63ebe68704df23185b6b5e65b4` 选择性加入本次云端改动，没有整包部署开发分支。
- 原生产标签为 `seo-cabe422e-20261031`；当前生产后端、前端均已只读核验对应 2176 源及本次标签。

## 上线范围

- 编辑岗位可以修改招聘平台，保留原岗位身份、历史和统计。
- 正式增加前程无忧平台；人才推荐、搜索人才与智联搜索由新版本地客户端适配，原智联推荐流程继续兼容。
- 复用已经打开的支持页，无需另选页面；URL和选择器兼容旧配置及对象/数组，宽窄窗口使用各自可见按钮。
- 云端对旧客户端的 `reuse_urls` 兼容仅在 HTTP400、`INVALID_REQUEST`、精确未知字段三项同时满足时回退一次原请求；其他错误不重试。
- Mac、Windows共用功能代码。本次只发布Mac安装包，Windows安装包随后由用户处理，没有加入系统功能白名单。
- 未上线开发分支的独立自动回复云端接口、0104/0105迁移或公开推荐报告功能。前程独立自动回复与附件下载未开放。

## 唯一数据库迁移

- 新增文件：`0106_zhaopin_search_51job_entries.sql`。
- SQL SHA256：`0ebfa7dcd1441b7db35e38c037a64173e1b0ffd851e5e6541e04fe66510f7307`。
- 只新增缺失的 `platform.51job`，并合并智联/前程的推荐、搜索地址和 `auth.reuse_urls`；保留旧入口、选择器、其他字段及原开关，不新增表结构。
- 正式只读回查确认0106账本恰有1条，0104/0105合计0条；公网平台配置返回前程已开放，两个平台各有两个支持地址。
- 生产PostgreSQL及Redis的容器ID、启动时间与发布前完全一致；没有重建数据服务。

## 隔离演练与切换

- 使用本次专用固定0106演练，完整生产dump只恢复到内部隔离网络。候选通过真实 `RunMigrations` 执行SQL，不手工补账本。
- 候选启动窗口仅临时授配置所需列、账本filename插入及既有迁移检查所需CREATE；health后撤回，并检查表级、列级写权限和CREATE均归零。
- 所有其他业务表的完整行摘要、行数、全部旧账本与数据库结构保持不变；两个平台的配置变动逐字段核对。
- 在同一迁移后恢复库验证完整旧前后端回退组合，全部表、完整账本和结构继续保持一致。
- 成功证据位于 `/opt/goodhr-release/platforms-2176fac5-20261102/isolated-validation.json`。
- 本次兼容登记位于同目录 `retention-registry.json`；配对证据为 `rehearsal/candidate-pair-summary.json`、`rehearsal/rollback-pair-summary.json`。
- 通过原正式 `--activate-only` 入口使用同一候选镜像激活；切换前另做完整数据库备份，保留原环境、挂载和网络参数。
- 生产结果和日志位于同目录 `production-result.json`、`activation-run.log`。显式保留回退材料，没有历史镜像或备份广域清理。

## 首次失败材料

- 首个候选为 `e8eb7dbae0b2c3230f8a3e12dcda3b57539f113e`，标签 `platforms-e8eb7dba-20261102`。
- 其隔离演练因服务器Python3.8不接受PostgreSQL短时区/可变小数时间格式而安全失败，未激活、未修改生产数据库或服务。
- 修正仅规范化校验工具的时间字符串，仍强制带时区及真实启动窗口；随后使用新源码、新标签和新生产dump从头演练，没有复用已迁移失败库或伪造成功证据。
- 失败dump、日志及证据保留在 `/opt/goodhr-release/platforms-e8eb7dba-20261102/`。
- 失败克隆pgdata另存完整归档：`/www/wwwroot/goodhr-production/backups/platforms-e8eb7dba-20261102-failed-rehearsal/pgdata.tar.gz`。
- 归档SHA256：`60812bee65ad1ab39f46ab64c400c96162a8e0d1b315d321516d24a5b0c1c198`；1696个成员的内容、uid/gid和模式已核验，确认原副本未变化后才移除该本轮已停止克隆的原副本。
- 本次仅对已核验ID、标签及挂载的隔离PG做WAL容量维护；生产PG未动，SQL数据/结构核验未放宽。

## Mac可选更新

- 版本 `6.2.12.2`，Apple Silicon/Intel通用；两架构隔离启动和版本回报已核验，未替换用户当前正式安装程序。
- 6.2.12系列此次为可选更新，保留既有最低版本要求，没有新增强制升级门槛。
- 安装包：[Mac通用pkg](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.2-mac-universal-0e8ee290.pkg)。SHA256为 `0e8ee2909af348d0adfbd53ee9d9a4a0b89b3f7a0eba9b502db96b0fe6c34b8d`。
- 更新压缩包：[Mac通用zip](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.2-darwin-universal-89d21209.zip)。SHA256为 `89d2120916f95bc787a4108dc4f4871362f2d53bdcdc8b4e45ac47cb4f51d986`。
- 两文件均通过完整公网下载校验，更新说明列出前程双页、智联搜索、页面复用、窗口兼容、岗位平台修改及书签调整。
- 更新记录新增1条Mac记录；原54条内容和顺序原样保留，共55条。新记录的Windows地址为空，Windows最新仍为 `6.2.12.1`，原文件和策略未改。

## 公开验收与边界

- 首页、下载、功能及三个招聘专题均200；平台配置、更新记录API均200，匿名私有账号接口仍401。
- 公网 `/health` 继续404；正式后端和前端的内部健康检查通过，没有为验收暴露内部健康入口。
- 发布前已通过195项发布工具测试、云端岗位测试、前端144项单测、TS、38页构建、13项针对UI及2项后台宽窄矩阵。隔离启动和匿名路由验证不代替真实业务写入或新增招聘消息实发。
- 本机公开验收和最终生产回查证据：`/tmp/goodhr-publish-20261102/public-release-verification.json`、`server-post-release-tool.json`、`public-mac-artifacts-verified.json`。
- 物理Intel Mac和Windows新安装包尚未在本次实机验证；两端功能代码已同步，Windows包不属于本次发包范围。
