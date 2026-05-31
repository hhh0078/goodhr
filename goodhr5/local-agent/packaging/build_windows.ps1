# 本脚本用于在 Windows 上打包 GoodHRLocalAgent.exe。

$ErrorActionPreference = "Stop"

Set-Location (Join-Path $PSScriptRoot "..")

python packaging\prepare_vendor.py --platform win --no-extract
python -m PyInstaller --clean --noconfirm --distpath dist --workpath build packaging\GoodHRLocalAgent.spec
powershell -ExecutionPolicy Bypass -File packaging\create_windows_shortcut.ps1

Write-Host "打包完成：dist\GoodHR"
