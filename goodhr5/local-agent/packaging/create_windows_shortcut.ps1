# 本脚本用于为 Windows 打包产物创建桌面快捷方式。

$ErrorActionPreference = "Stop"

$AppName = "GoodHR招聘助手"
$ProjectRoot = Resolve-Path (Join-Path $PSScriptRoot "..")

<#
查找 PyInstaller 实际生成的 Windows 可执行文件。
参数 DistDir 表示打包输出目录。
返回找到的 exe 完整路径。
#>
function Find-GoodHRExecutable {
    param (
        [string]$DistDir
    )

    if (-not (Test-Path $DistDir)) {
        throw "Dist directory not found: $DistDir"
    }

    $PreferredNames = @(
        "$AppName.exe",
        "GoodHRLocalAgent.exe"
    )

    foreach ($Name in $PreferredNames) {
        $Match = Get-ChildItem -Path $DistDir -Filter $Name -Recurse -File -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($Match) {
            return $Match.FullName
        }
    }

    $AnyExe = Get-ChildItem -Path $DistDir -Filter "*.exe" -Recurse -File -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($AnyExe) {
        return $AnyExe.FullName
    }

    throw "Executable file not found in dist directory: $DistDir"
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

$DistDir = Join-Path $ProjectRoot "dist"
$ExePath = Find-GoodHRExecutable -DistDir $DistDir
$Desktop = [Environment]::GetFolderPath("Desktop")
$ShortcutPath = Join-Path $Desktop "$AppName.lnk"
New-GoodHRDesktopShortcut -ShortcutPath $ShortcutPath -TargetPath $ExePath -WorkingDirectory (Split-Path $ExePath)

Write-Host "Desktop shortcut created: $ShortcutPath"
Write-Host "Shortcut target: $ExePath"
