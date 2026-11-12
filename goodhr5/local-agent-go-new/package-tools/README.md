<!-- 文件作用说明：说明 Windows、macOS 的统一打包入口、环境准备、命令和成品检查方法。 -->

# GoodHR 本地程序打包说明

以后打包从这个文件夹进入。这里的脚本会调用项目原来的构建脚本，请保留完整项目，不要只复制这个文件夹到其他电脑。

| 文件 | 用途 |
| --- | --- |
| `package-windows.bat` | 在 Windows 电脑打包 Windows x64 版 |
| `package-macos.command` | 在 Mac 电脑打包 Apple 芯片、Intel 通用版 |
| `安装教程图片.jpg` | Windows ZIP 必须包含的安装说明图片；已随项目保存 |

## 打包前准备

1. 先停止打包电脑上的 GoodHR 招聘任务，保存正在修改的文件。
2. 安装 Go（版本满足项目 `go.mod` 要求）和 Node.js 22 或更高版本，确保终端能执行 `go version`、`node --version`、`npm --version`。
3. Windows 还需要安装 Inno Setup 6。Mac 需要 Xcode Command Line Tools；终端执行 `xcode-select --install` 后按提示安装，已安装可跳过。Mac 不需要 Inno Setup。
4. 保留项目的 `goodhr5/environments/production.env` 和 `test.env`，它们决定包连接哪个网站。
5. 脚本会联网安装锁定的 Worker 依赖。默认 npm 镜像为 `https://registry.npmmirror.com`；Go 未配置 `GOPROXY` 时使用 `https://goproxy.cn,direct`，已配置则沿用。可以通过 `GOODHR_NPM_REGISTRY`、`GOPROXY` 指定自己的镜像。

下面以版本 `6.2.12.5` 举例，下次换成你要打包的版本号即可。版本号建议只用数字和点。

## Windows：生成安装程序和图片的 ZIP

必须在 **Windows 电脑**上执行。用资源管理器打开本文件夹，在地址栏输入 `cmd` 并回车，然后输入：

```bat
REM 正式版：连接正式网站
package-windows.bat 6.2.12.5 build

REM 测试版：连接测试网站
package-windows.bat 6.2.12.5 dev
```

如果打开的是 PowerShell，命令前加 `.\`，例如：

```powershell
.\package-windows.bat 6.2.12.5 build
```

打包完成后，在项目的 `goodhr5/local-agent-go-new/release/` 文件夹找：

```text
goodhr-local-agent-v6.2.12.5-windows-x64.zip
├── GoodHR-Local-Agent-Setup-v6.2.12.5-windows-x64.exe
└── 安装教程图片.jpg
```

**发给 Windows 用户的是这个 ZIP。** 用户解压后先看图片，再双击里面的安装程序。脚本会检查 ZIP 内这两个文件都存在且大小与原文件一致，缺少任何一个都会报错，不会提示成功。

同一 `release/` 目录也会保留单独的 `.exe` 和供安装器构建使用的程序目录。测试版文件名会多一个 `-test`，例如 `goodhr-local-agent-v6.2.12.5-test-windows-x64.zip`，里面安装程序的名字也会多 `-test`。

要更新说明图片，替换本文件夹内的 `安装教程图片.jpg` 后重新打包即可；不要改文件名。图片不存在或为空时会提前停止。

## Mac：生成 Apple 芯片、Intel 通用包

必须在 **Mac 电脑**上执行。打开终端，进入本文件夹：

```bash
cd "/Users/Zhuanz/Downloads/goodHR/goodhr5/local-agent-go-new/package-tools"

# 正式版
zsh ./package-macos.command 6.2.12.5 build

# 测试版
zsh ./package-macos.command 6.2.12.5 dev
```

如果项目以后换了位置，把上面的 `cd` 路径改成实际位置。也可以在终端输入 `cd `，将本文件夹拖进去，再按回车。

打包完成后，在 `goodhr5/local-agent-go-new/release/` 找：

| 成品 | 用途 |
| --- | --- |
| `GoodHR-Local-Agent-Setup-v6.2.12.5-mac-universal.pkg` | 通常发这个，双击安装 |
| `goodhr-local-agent-v6.2.12.5-darwin-universal.zip` | 含 `.app` 的压缩包 |
| `GoodHR Local Agent.app` | 本机生成的应用 |

Mac 的 PKG 和 ZIP **都不放安装说明图片**。同一包包含 Apple 芯片和 Intel 的 Go 主程序及对应的 Worker 原生依赖，脚本会验证主程序双架构。测试版 PKG、ZIP 文件名会多 `-test`；未签名安装包可能需要在“系统设置 → 隐私与安全性”中允许打开。

## 查看打包文件的 SHA256

SHA256 是文件的校验值，用来核对文件是否一致。**准备上传哪个文件，就计算哪个文件的 SHA256**；ZIP、里面的 EXE、Mac 的 PKG 各有自己的校验值，不能混用。重新打包后需要重新计算。

下面的命令都在本文件夹 `package-tools` 中执行，把 `6.2.12.5` 换成实际版本号；测试包记得加 `-test`。

### Windows

在本文件夹打开 PowerShell，执行下面的命令，只会输出要复制的 SHA256：

```powershell
# 查看发给用户的 ZIP
(Get-FileHash -LiteralPath "..\release\goodhr-local-agent-v6.2.12.5-windows-x64.zip" -Algorithm SHA256).Hash

# 查看单独安装程序 EXE
(Get-FileHash -LiteralPath "..\release\GoodHR-Local-Agent-Setup-v6.2.12.5-windows-x64.exe" -Algorithm SHA256).Hash
```

如果打开的是 CMD，也可以使用系统自带命令：

```bat
certutil -hashfile "..\release\goodhr-local-agent-v6.2.12.5-windows-x64.zip" SHA256
```

CMD 输出中只复制中间那一行 64 位校验值，不要复制前后的说明文字。

### Mac

在终端进入本文件夹后执行：

```bash
# 查看 PKG 安装包
shasum -a 256 "../release/GoodHR-Local-Agent-Setup-v6.2.12.5-mac-universal.pkg"

# 查看 ZIP 压缩包
shasum -a 256 "../release/goodhr-local-agent-v6.2.12.5-darwin-universal.zip"
```

每行输出前面是 64 位 SHA256，后面是文件路径，**只复制前面的校验值**。也可以输入 `shasum -a 256 `（末尾留一个空格），把要检查的文件拖进终端，再按回车。

上传后下载一份，再用相同方法计算并与上传前的值比较；完全相同就表示两份文件一致。SHA256 的字母大小写不影响比较。

## 常见情况

- **提示要填写 dev 或 build**：最后一个参数必填，`build` 是正式版，`dev` 是测试版。
- **找不到 go、node、npm 或 Inno Setup**：先安装对应工具，再重新打开终端执行。Mac 不需要 Inno Setup。
- **依赖下载失败**：检查网络和镜像地址，再执行相同命令重试。
- **要找成品**：在 `local-agent-go-new/release/`，不在这个工具文件夹。
- **同版本重新打包**：会覆盖该版本、该环境的旧成品，需要保留旧包时先复制到别处。
- **打包和发布**：这些命令只生成本地文件；不会上传文件，也不会修改线上下载地址或触发用户升级。

## 验证范围

Windows 的安装程序由 `scripts/package-windows.ps1` 和 `packaging/GoodHRLocalAgent.iss` 生成，ZIP 内容由 Windows 脚本重新打开校验；Mac 复用 `scripts/package-release.sh` 的双架构构建流程。旧路径的脚本仍可继续使用。

2026-11-11 整理时已通过 Windows x64、macOS arm64、macOS amd64 的 Go 编译、Worker 编译和现有安装器测试；Mac 入口已检查脚本语法、错误参数、中文及空格路径、参数传递和退出码。项目内图片已核对与原图片完全一致。

本次未运行完整的 Windows / Mac 安装包构建，也未执行三种设备的安装和运行测试。Windows ZIP 的生成与重新打开检查需在 Windows 上实测；交叉编译不能代替安装验证，分发前请在对应设备检查安装、启动与浏览器运行。
