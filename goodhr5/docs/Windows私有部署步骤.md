<!-- 文件作用说明：给部署人员提供客户 Windows 电脑上安装、配置、启动和使用 GoodHR 私有系统的可执行步骤。 -->
# 在客户 Windows 电脑部署

本文只使用 `codex/private-lan-deployment` 分支。服务器是客户内网的一台 Windows 电脑；员工用浏览器打开它的内网地址。首次准备需要联网下载镜像和组件，之后 GoodHR 服务与组件下载走内网，招聘网站仍需联网。

## 1. 准备部署电脑

- 建议使用 Windows 11 x64、至少16GB内存、至少30GB空闲磁盘，并为这台电脑保留固定内网 IP。这是本项目部署建议，不是 Docker 最低配置。
- 安装并启动 [Docker Desktop](https://docs.docker.com/desktop/setup/install/windows-install/)，使用 WSL 2 和 Linux 容器。首次安装按提示启用虚拟化、WSL并重启。
- 安装 [Python 3](https://www.python.org/downloads/windows/)，安装时勾选加入 PATH。它用于运行已有镜像检查脚本。
- 当前用户应能在 PowerShell 中运行 `docker version`、`docker compose version` 和 `python --version`。

Docker Desktop 需要保持运行；主机需要保持开机，使用期间不要休眠。这里的 Docker Desktop 方案适用于其支持的 Windows 桌面系统，不直接用于 Windows Server。

## 2. 解压部署包并填写配置

把私有分支部署包解压到例如 `D:\GoodHR-Private`，进入其中的 `goodhr5` 目录。下面命令均在该目录的 PowerShell 中执行。

```powershell
Copy-Item .\environments\private.env.example .\environments\private.env
notepad .\environments\private.env
```

将以下字段改成客户的真实值，例如：

```dotenv
GOODHR_PUBLIC_BASE_URL=https://192.168.1.100:8443
GOODHR_HTTPS_PORT=8443
GOODHR_SUPER_ADMINS=admin@example.com
GOODHR_PRIVATE_ADMIN_PASSWORD=客户自己设置的密码
GOODHR_DB_PASSWORD=至少24位随机英文字母和数字
```

邮箱是登录账号标识；管理员首次密码登录不需要公网邮箱验证码。修改 IP 时，上面的服务地址必须同步修改。其余字段初次保持模板值即可；不要把配置里的密码发到群里。

## 3. 启动系统

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy-private.ps1
```

脚本会构建前后端和私有插件、启动数据库，下载并校验兼容组件到 `private-data\assets`，生成内网 HTTPS 证书。首次需要下载较多文件，等待脚本报告启动完成，不要中途关闭 Docker Desktop。

如果提示没完成，查看后端日志，按具体错误修好后重试原命令：

```powershell
docker compose --env-file .\environments\private.env -f .\docker-compose.private.yml logs --tail 80 backend
```

在管理员 PowerShell 中仅向本地网段放行服务端口：

```powershell
New-NetFirewallRule -DisplayName "GoodHR 内网8443" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8443 -Profile Private,Domain -RemoteAddress LocalSubnet
```

## 4. 每台使用电脑信任证书并登录

将部署电脑生成的 `goodhr5\private-data\GoodHR-private-root.crt` 复制给每台使用电脑，只复制这个公开根证书，不复制整个 Caddy 目录。

在使用电脑上运行（路径改为实际位置）：

```powershell
certutil -user -addstore Root .\GoodHR-private-root.crt
```

重启浏览器和 GoodHR 本地程序，再打开 `https://192.168.1.100:8443`，用配置中的管理员邮箱和密码登录。浏览器提示当前站点连接本机/内网的权限时允许。

## 5. 配置 AI 和浏览器插件

在“个人配置 → AI配置”填写内网模型地址、模型名称和 Key，先测试再保存。AI 地址必须能从后端容器和执行电脑访问；模型在另一台电脑时填写那台电脑的内网 IP，不要填写 `localhost`。无鉴权接口可用任意非空 Key 占位；BOSS 详情识别需要支持图片的模型。

插件从内网下载页下载，默认连接本次配置的地址。必要时在插件工具栏设置页填写同一 HTTPS 地址并重新登录。Chrome 开发包按包内安装说明解压加载；Firefox 未签名包按临时加载说明使用。

## 6. 准备 Windows 私有执行器

浏览器插件和系统网页准备好后，实际运行招聘任务还需要一个绑定同一内网地址的 Windows 客户端安装器。官网通用版的默认服务地址不同，不能直接当成本企业私有包。

在一台 Windows 打包电脑准备 Go、Node.js 22+、Inno Setup 6，使用部署包内相同的 `environments\private.env`，然后执行：

```powershell
.\local-agent-go-new\scripts\package-windows.ps1 -Version 6.4.0 private
```

将生成的私有 `.exe` 安装器和该次发布目录中的 `build-info.json` 放到部署电脑。部署电脑安装 Node.js 后可运行一次登记命令（或由打包人员准备好 `private-data\assets` 和 `clients.json` 一起交付）：

```powershell
node .\scripts\register-private-client.mjs .\environments\private.env D:\包路径\安装器.exe D:\包路径\build-info.json win 6.4.0
docker compose --env-file .\environments\private.env -f .\docker-compose.private.yml restart backend
```

普通员工只安装这个私有 `.exe`，不需要安装 Go、Node 或 Inno Setup。让本地程序运行，再在内网后台连接，安装内网提供的运行组件，登录招聘网站并开始任务。

## 7. 交付前检查

- 另一台内网电脑能用 HTTPS 正常打开后台并登录。
- AI测试成功，插件能登录并保存合成测试简历。
- Windows客户端能连接后台，组件下载地址为客户内网地址，能打开招聘浏览器。
- 重启部署电脑并启动 Docker Desktop，服务与已缓存组件仍可使用。
- 实际招聘任务在客户 Windows 上完成一次验收；本轮 Mac 上的编译和测试部署不能代替这一步。

详细的 Mac 证书、双平台打包、持久化和备份说明见 [私有内网部署](私有内网部署.md)。
