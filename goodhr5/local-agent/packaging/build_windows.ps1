# 本脚本用于在 Windows 上打包 GoodHRLocalAgent.exe。

$ErrorActionPreference = "Stop"

Set-Location (Join-Path $PSScriptRoot "..")

python packaging\prepare_vendor.py --platform win
python -m PyInstaller --clean --noconfirm --distpath dist --workpath build packaging\GoodHRLocalAgent.spec

Write-Host "打包完成：dist\GoodHRLocalAgent"
