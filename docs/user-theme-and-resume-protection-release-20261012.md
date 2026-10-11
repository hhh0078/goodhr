<!-- 文件作用：记录后台自由主题与简历保护提示的代码、测试、正式发布及前端回退证据。 -->

# 后台自由主题与简历保护提示发布记录（2026-10-12）

2026-10-12 01:31（北京时间）已切换正式前端至 [GoodHR](https://goodhr.58it.cn)。构建、候选页面、正式页面、运行配置及本轮 14 项不同界面用例检查通过。

## 改动与范围

- 后台默认绿色，所有会员均可自由选择 `green` 松绿色、`ink` 深墨黑、`gold` 黑金色；会员状态变化不覆盖用户选择。
- 主题按账号在当前浏览器保存，邮箱优先作为账号标识；公开页保持绿色，非法缓存及缓存读写失败有安全回退处理。
- 简历库搜索栏上方新增提示，移动端布局已验证：

  > 请勿非法买卖简历。平台不会出售您的简历，分享权限由您掌握。

本轮仅发布 Next.js 前端。后端、数据库、本地客户端不更新，数据库迁移数量为 **0**。

## 代码与本地验证

| 项目 | 记录 |
| --- | --- |
| 主题功能提交 | `e6873e09` |
| 主分支合并及部署候选提交 | `f6b346ef95308583f65c2c8700e85161ff5392f6` |
| 前端源码 tar SHA256 | `9b4bff6bf2529887be896bec96dc570311f15cf8680f60160000ff4dbb34ccc0` |
| Node 测试 | 26/26 通过 |
| TypeScript 检查 | `tsc` 通过 |
| 主题 UI 回归 | 12/12 通过 |
| 简历搜索 URL、详情及移动布局回归 | 2/2 通过 |
| 提示最终布局复测 | 1/1 通过 |
| 差异格式检查 | `diffcheck` 通过 |

以上本地检查与下面的生产运行检查分别记录，业务接口模拟测试不代表真实账号写入验收。

## 发布目标与旧版本基线

| 项目 | 记录 |
| --- | --- |
| 服务器 | `ldclouda31539` / `43.228.79.108` |
| 发布目录 | `/opt/goodhr-release/user-theme-20261012` |
| 正式前端容器名 | `goodhr-production-frontend-1` |
| 旧前端容器 ID | `0225543ffade269498b1fd94a896e8f31ef6008db69013dcbcaf28fbed24b761` |
| 旧前端镜像 ID | `sha256:8df12f7ac1a9a26b8bd7105d9a0316900ea2f756cb483a7ac027ff73d9565e72` |
| 旧容器启动时间 | `2026-10-10T16:35:27.214049433Z` |
| 候选容器名 | `goodhr-user-theme-20261012-candidate` |
| 旧容器备份名 | `goodhr-production-frontend-1-backup-user-theme-20261012` |

上述服务器及旧版本完整身份已在本轮连接服务器核对。旧容器完整配置保存于发布目录的 `before-inspect.private.json`，仅 root 可读。

## 线上证据

| 项目 | 当前记录 |
| --- | --- |
| 生产镜像构建结果及日志 | Next.js 16.2.9 `--webpack` 编译、类型检查、34 个页面生成通过；`frontend-build.private.log`、`frontend-image.private.log` |
| 新镜像 ID | `sha256:1307be7ba853290fc89cbf872fcab81556525ad9f12b158842440718c4c71cbb` |
| 镜像源码标签 | `org.opencontainers.image.revision=f6b346ef95308583f65c2c8700e85161ff5392f6` |
| Next.js Build ID | `00PNGbvG_m2aXAq8uwhhT` |
| 候选容器验证结果 | `9e55f9adc9a6310ec559b7be8228b5e7f015dc46107ef1d632a6d28f94d3cce1`；三个页面、12 份静态资源通过，检查后停止并删除本轮候选 |
| 正式新容器 ID | `eba4534209e11bc2f21b634d71a963fa24b259ba8c19a675feee11d2e45cd61d` |
| 正式启动时间 | `2026-10-11T17:31:02.53745359Z`（北京时间 2026-10-12 01:31） |
| 线上默认绿色、主题选择及账号隔离 | 三类会员默认绿色，可选三色；账号 A → B → A 隔离、刷新保存通过 |
| 线上会员状态变化与公开页绿色 | 会员及邀请编号刷新不覆盖主题；真实 SPA 跳转公开页及注销后恢复绿色通过 |
| 线上简历保护提示及手机布局 | 提示在搜索栏上方，1920×1000 与 390×844 截图确认，搜索 URL、详情及窄屏检查通过 |
| 正式 HTTP 与资源检查 | 本机 5175 与公网 `/login`、`/admin/resumes`、`/download` 均 HTTP 200，12 份静态资源 SHA256 一致，运行 Build ID 相符 |
| 后端、PostgreSQL、Redis | ID、镜像、启动时间及完整 Config/HostConfig/挂载均与发布前一致，运行正常，重启次数均为 0 |
| 后端及权限检查 | 本机 `127.0.0.1:8086/health` 为 200，未登录的消息摘要及简历库设置接口为 401 |
| 客户端更新配置 | 发布前后完整公开 JSON 摘要均为 `f071fd60aa79a7859fd6eb3ce32b5b9d6a5aba3bc4a21fa6cd9fa5b14e696dad`，没有发布客户端版本 |
| 旧容器保留与回退材料位置 | 原前端已停止并保留为上表备份名；配置、不可变镜像及脚本保留在本轮发布目录 |

本轮使用已存在的 Node 与依赖缓存镜像，核对 `package-lock.json` 的 LF SHA256 为 `60731c62cf4b2fb8516e035301fdca1c2889fea6ca1b709d2eec077266db98c7`，没有下载或安装依赖。构建限制 1 CPU、2 GiB 内存，构建期间正式应用持续运行。

生产交互回归使用真实生产 HTML 和静态资源，全部业务接口由测试替身拦截，使用假凭证和合成候选人，没有登录或写入真实账号。首轮 13/14 通过；剩余一项因模拟本地程序切换在线状态时没有等既有两秒探测缓存过期而失败。保留所有主题、账号及 SPA 断言，补齐阳性/阴性缓存的等待条件后定向复测 1/1 通过，业务代码没有因此修改。测试补充后的 TypeScript 与差异格式检查通过。

本地证据目录：`D:\work\goodhr-release-artifacts\20261012-user-theme\production-ui`，包含 `production-ui-evidence.json`、首轮 `playwright.log`、定向复测 `cache-retry-2/playwright.log` 及 trace；截图位于 `artifacts/resume-library/resume-protection-desktop.png` 和 `resume-protection-mobile.png`。后续文档和测试提交不改变本轮运行镜像及源码提交标签。

发布目录保留 `manifest.private.json`、`build-result.private.json`、`stage-result.private.json`、`deployment-result.private.json`、`production-final-verification.json`、前后完整容器快照及客户端更新配置快照。原准备脚本的挂载数组比较因 Docker 返回顺序变化而停止；已核对排序后的全部挂载字段一致，修正后重新校验全部配置，未覆盖准备材料。

修正版脚本 `frontend-theme-release.py` 的 SHA256 为 `77c8082999a860e6af34d58d69365a22dc847397ca7f08bb909b011a4fdf57d2`，原脚本保留为 `frontend-theme-release-original.py`。回退只恢复保存的原前端，不恢复数据库或操作后端服务：

```sh
python3 /opt/goodhr-release/user-theme-20261012/frontend-theme-release.py rollback --root /opt/goodhr-release/user-theme-20261012 --commit f6b346ef95308583f65c2c8700e85161ff5392f6
```
