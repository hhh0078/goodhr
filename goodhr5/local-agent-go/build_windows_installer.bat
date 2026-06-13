@echo off
REM Purpose: check Windows build tools and package GoodHR Go Local Agent installer.
setlocal EnableExtensions

set "ROOT_DIR=%~dp0"
set "VERSION=%~1"
if "%VERSION%"=="" set "VERSION=0.1.0"

echo [GoodHR] Start Windows installer build. Version: %VERSION%
echo [GoodHR] Project dir: %ROOT_DIR%
echo.

REM Check Go.
where go >nul 2>nul
if errorlevel 1 (
  echo [GoodHR][ERROR] Go was not found.
  echo Please install Go and reopen PowerShell or CMD.
  echo Download: https://go.dev/dl/
  pause
  exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set "GO_VERSION=%%i"
echo [GoodHR] Go found: %GO_VERSION%

REM Check Inno Setup 6.
set "ISCC_PATH="
where ISCC.exe >nul 2>nul
if not errorlevel 1 (
  for /f "tokens=*" %%i in ('where ISCC.exe') do (
    if not defined ISCC_PATH set "ISCC_PATH=%%i"
  )
)
if not defined ISCC_PATH if exist "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe" set "ISCC_PATH=%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe"
if not defined ISCC_PATH if exist "%ProgramFiles%\Inno Setup 6\ISCC.exe" set "ISCC_PATH=%ProgramFiles%\Inno Setup 6\ISCC.exe"

if not defined ISCC_PATH (
  echo [GoodHR][ERROR] Inno Setup 6 was not found.
  echo Please install Inno Setup 6 and run this script again.
  echo Download: https://jrsoftware.org/isdl.php
  pause
  exit /b 1
)
echo [GoodHR] Inno Setup found: %ISCC_PATH%

REM Check Worker directory and dependencies.
if not exist "%ROOT_DIR%worker-node\package.json" (
  echo [GoodHR][ERROR] worker-node\package.json was not found.
  echo Please make sure this script is in goodhr5\local-agent-go.
  pause
  exit /b 1
)

if not exist "%ROOT_DIR%worker-node\node_modules" (
  echo [GoodHR][ERROR] worker-node\node_modules was not found.
  echo Please install Node Worker dependencies first:
  echo.
  echo   cd /d "%ROOT_DIR%worker-node"
  echo   npm install
  echo.
  pause
  exit /b 1
)
echo [GoodHR] Worker dependencies found.

REM Check PowerShell.
where powershell >nul 2>nul
if errorlevel 1 (
  echo [GoodHR][ERROR] PowerShell was not found.
  pause
  exit /b 1
)

echo.
echo [GoodHR] Running installer build script...
powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT_DIR%packaging\build_windows_installer.ps1" -Version "%VERSION%"
if errorlevel 1 (
  echo.
  echo [GoodHR][ERROR] Build failed. Please check the error output above.
  pause
  exit /b 1
)

echo.
echo [GoodHR] Build completed.
echo [GoodHR] Installer output dir: %ROOT_DIR%dist-installer
echo.
pause
exit /b 0
