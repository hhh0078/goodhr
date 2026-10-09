<!-- 本文件记录 2026-10-09 仅前端 Docker 发布的实际产物、验证结果与回退位置。 -->
# GoodHR 前端 Docker 发布记录

正式站点：https://goodhr.58it.cn

服务器：`ldclouda31539 / 43.228.79.108`。正式切换及线上检查在北京时间 2026-10-09 21:07:52 至 21:08:03 完成；该时间包含验证过程，不代表准确的访问中断时长。

## 实际产物

- 代码提交：`32056c252ed8c0ffa744c82685e85f4d6a5fcc63`，分支 `codex/frontend-update-reconnect-20261009`，已推送并快进合并到 `main`。
- 镜像：`goodhr-production-frontend:antd-20261009-32056c25`。
- 镜像 ID：`sha256:a7b7bb101b4dd9d6990a428cc966b657760988de2cca0a8b1534d363971cb39d`，实际大小 234075859 字节。
- 正式容器：`goodhr-production-frontend-1`，ID `a8ea9f9910f14ba36f9d78823b05a57285294cc3669a8aaca87aad41706648d3`。
- Next.js BUILD_ID：`uWj23Db6eHGajV1EYYRXy`。候选和正式容器使用同一镜像及同一构建标识。
- 运行端口 `127.0.0.1:5175 → 3000`、原网络、`unless-stopped` 重启策略、JSON 日志轮转和生产域名环境变量均保留，无挂载。

服务器仅从该提交归档 `goodhr5/cloud/frontend-next` 到隔离目录构建，没有更新服务器完整工作区。构建使用国内 npm 镜像，1GB 容器内存、768MB Node 堆、1 个 CPU 和 1 个 Next 构建 worker；这些构建参数未进入运行镜像。

## 修复及测试

- 安装期间断线继续重连；重新连接、账号绑定和版本同步完成后才结束更新弹框。慢状态请求串行读取，重复提交、旧响应回写和离开后台后的回写均有保护。
- 覆盖绑定 409、health 暂时失败、两个版本来源为空，以及新版 idle 快照后再次断线的重试。
- 会员激活使用后端已有接口；本地绑定显示具体错误并允许安全重试。补齐被运行日志规则忽略的页面 CSS，修复手机长备注提示挡住点击。
- 最终受限生产构建和 TypeScript 检查通过，32 个路由生成；15 项逻辑测试、38 组页面回归、10 组更新专项测试和 11 项独立更新恢复模拟检查全部通过。
- 候选和正式 HTTPS 均实际检查 30 个页面、6 个公开页面的服务端正文/元信息/canonical/JSON-LD、5 个旧地址的 308 跳转及 20 个静态资源，均正常。后台 HTTP 检查证明页面可载入，业务写入流程使用隔离模拟测试。
- Linux 容器实际加载 Node v22.23.3 和 sharp 0.34.5 成功。真实公开登录页面零页面、控制台及资源错误，未登录或提交。
- 发布后本机 `/health`、`/api/v1/runtime/status`、`/api/v1/app-update/status` 只读请求均 HTTP 200，CORS 来源为生产域名，版本仍为 6.2.10.1，更新状态 idle。安装和绑定写入使用模拟接口，未执行真实安装、岗位任务、支付或数据修改。
- 正式 HTTPS 页面在隔离 Chrome 中的真实本机联动检查也通过（4.5 秒）：8 次 GET 全部 200，三个接口各有完整响应/版本/CORS 证据，页面显示新版 Ant Design 和 6.2.10.1，零本地网络失败及页面异常；未修改浏览器权限。账号数据、3 次 POST 与 OPTIONS 使用模拟，未把预检模拟当作真实预检验证。诊断采集器记录 1 条重复 health 正文读取超时警告，另一份 health 完整响应和页面版本核对通过；两个外部资源阻断日志来自主动屏蔽百度统计。

## 保持原样的服务

前后独立对比后端、PostgreSQL、Redis 的完整容器 ID、镜像 ID 和启动时间均相同，未重启。后端健康检查正常。服务器 Git 工作区 HEAD 仍为 `e3b966572c0f5d1770446bea87921aa34bad7ca7`。

本地程序发布配置在切换前后完整响应 SHA256 均为 `11394c4d8c4af1385de60229b3a89a914b1ae812a7148ec58f32ac07726b21dd`，仍为 38 个版本，最新 6.2.10.1；没有更新本地安装包或发布元数据。

## 回退与证据

服务器备份目录：`/www/wwwroot/goodhr-production/backups/frontend-antd-20261009-205056/`。

其中保留旧容器配置、前后 BUILD_ID、服务基线、更新配置快照、构建日志、候选/正式 HTTP 检查结果和 `deployment-result.json`。旧镜像和已停止的旧容器 `goodhr-production-frontend-backup-20261009-205056` 保留，可运行该目录的 `rollback-frontend.sh` 仅恢复前端；脚本语法检查通过，当前正式前端正常，因此未执行真实回退。

临时候选容器已删除，旧前端回退容器保留。本机测试截图及结果保存在已忽略的 `artifacts/antd-update-qa/after-fix/` 和 `artifacts/production-frontend-check/`。原工作区已有的 `lib/admin-api.ts` 未提交改动与附件均保留，未加入本次镜像。
