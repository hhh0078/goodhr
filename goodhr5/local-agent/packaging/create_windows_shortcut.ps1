# 本脚本用于为 Windows 打包产物创建桌面快捷方式。

$ErrorActionPreference = "Stop"

$AppName = "GoodHR招聘助手"
$ProjectRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$ExePath = Join-Path $ProjectRoot "dist\$AppName\$AppName.exe"

if (-not (Test-Path $ExePath)) {
    throw "未找到可执行文件：$ExePath"
}

<#
创建 Windows 桌面快捷方式。
参数 ShortcutPath 表示快捷方式保存路径。
参数 TargetPath 表示快捷方式指向的程序路径。
参数 WorkingDirectory 表示程序工作目录。
无返回值。
#>
function New-GoodHRDesktopShortcut {
    param (
        [string]$ShortcutPath,
        [string]$TargetPath,
        [string]$WorkingDirectory
    )

    $Shell = New-Object -ComObject WScript.Shell
    $Shortcut = $Shell.CreateShortcut($ShortcutPath)
    $Shortcut.TargetPath = $TargetPath
    $Shortcut.WorkingDirectory = $WorkingDirectory
    $Shortcut.IconLocation = "$TargetPath,0"
    $Shortcut.Save()
}

$Desktop = [Environment]::GetFolderPath("Desktop")
$ShortcutPath = Join-Path $Desktop "$AppName.lnk"
New-GoodHRDesktopShortcut -ShortcutPath $ShortcutPath -TargetPath $ExePath -WorkingDirectory (Split-Path $ExePath)

Write-Host "桌面快捷方式已创建：$ShortcutPath"
