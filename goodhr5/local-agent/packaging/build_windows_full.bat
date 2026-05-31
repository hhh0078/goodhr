@echo off
REM This script builds GoodHR Local Agent on Windows.

setlocal enabledelayedexpansion
chcp 65001 >nul

cd /d "%~dp0\.."

echo ==^> Current dir: %CD%

call :find_python
if errorlevel 1 (
  echo ERROR: Python 3.10+ was not found.
  echo Please install Python 3.12 from https://www.python.org/downloads/windows/
  echo When installing, enable "Add python.exe to PATH".
  echo If Windows opens Microsoft Store, disable python.exe aliases in:
  echo Settings ^> Apps ^> Advanced app settings ^> App execution aliases
  exit /b 1
)

for /f "delims=" %%v in ('%SYSTEM_PYTHON% --version') do set "SYSTEM_PYTHON_VERSION=%%v"
echo ==^> Using Python: %SYSTEM_PYTHON_VERSION%

if exist ".venv\Scripts\python.exe" (
  for /f "delims=" %%v in ('".venv\Scripts\python.exe" -c "import sys; print('yes' if sys.version_info >= (3, 10) else 'no')"') do set "VENV_OK=%%v"
  if not "!VENV_OK!"=="yes" (
    echo ==^> Existing .venv Python is lower than 3.10, rebuilding
    rmdir /s /q .venv
  )
)

if not exist ".venv\Scripts\python.exe" (
  echo ==^> Creating Python virtualenv .venv
  %SYSTEM_PYTHON% -m venv .venv
  if errorlevel 1 exit /b 1
)

set "PYTHON=.venv\Scripts\python.exe"

echo ==^> Configure pip mirror
"%PYTHON%" -m pip config set global.index-url https://mirrors.aliyun.com/pypi/simple >nul
"%PYTHON%" -m pip config set install.trusted-host mirrors.aliyun.com >nul

echo ==^> Upgrade pip
"%PYTHON%" -m pip install -U pip
if errorlevel 1 exit /b 1

echo ==^> Install dependencies
"%PYTHON%" -m pip install -e ".[packaging]"
if errorlevel 1 exit /b 1

echo ==^> Prepare Windows CloakBrowser
"%PYTHON%" packaging\prepare_vendor.py --platform win --no-extract
if errorlevel 1 exit /b 1

echo ==^> Build with PyInstaller
"%PYTHON%" -m PyInstaller --clean --noconfirm --distpath dist --workpath build packaging\GoodHRLocalAgent.spec
if errorlevel 1 exit /b 1

echo ==^> Create desktop shortcut
powershell -ExecutionPolicy Bypass -File packaging\create_windows_shortcut.ps1
if errorlevel 1 exit /b 1

echo ==^> Build complete
echo Output: %CD%\dist\GoodHR招聘助手

endlocal
exit /b 0

:find_python
for %%C in ("py -3.12" "py -3.11" "py -3.10" "python" "python3") do (
  %%~C -c "import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)" >nul 2>nul
  if not errorlevel 1 (
    set "SYSTEM_PYTHON=%%~C"
    exit /b 0
  )
)
exit /b 1
