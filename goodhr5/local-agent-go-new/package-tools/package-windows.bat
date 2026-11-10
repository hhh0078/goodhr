@echo off
REM 文件作用说明：从统一打包工具目录调用现有 Windows 打包脚本，透传版本号和环境并返回退出码。
setlocal EnableExtensions
call "%~dp0..\scripts\package-windows.bat" %*
exit /b %ERRORLEVEL%
