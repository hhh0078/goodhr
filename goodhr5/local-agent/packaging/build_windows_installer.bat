@echo off
REM 本文件负责在 Windows 上构建 GoodHR Local Agent 安装器。

setlocal enabledelayedexpansion
chcp 65001 >nul

cd /d "%~dp0\.."

echo ==^> Build GoodHR folder package
call packaging\build_windows_full.bat
if errorlevel 1 exit /b 1

call :find_inno
if errorlevel 1 (
  echo ERROR: 未找到 Inno Setup 编译器 ISCC.exe
  echo 请先安装 Inno Setup: https://jrsoftware.org/isdl.php
  echo 或将 ISCC.exe 加入 PATH 后重试。
  exit /b 1
)

echo ==^> Build Windows installer
if not exist dist-installer mkdir dist-installer
"%ISCC%" packaging\GoodHRLocalAgent.iss
if errorlevel 1 exit /b 1

echo ==^> Installer complete
echo Output: %CD%\dist-installer

endlocal
exit /b 0

:find_inno
for %%P in (
  "ISCC.exe"
  "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe"
  "%ProgramFiles%\Inno Setup 6\ISCC.exe"
) do (
  %%~P /? >nul 2>nul
  if not errorlevel 1 (
    set "ISCC=%%~P"
    exit /b 0
  )
)
exit /b 1
