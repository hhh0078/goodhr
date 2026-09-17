; 文件作用说明：使用 Inno Setup 生成 Windows x64 GoodHR 本地程序安装器。
#define MyAppName "GoodHR Local Agent"
#ifndef MyAppVersion
#define MyAppVersion "6"
#endif
#define MyAppExeName "goodhr-local-agent.exe"
#define MyPackageDir "..\release\goodhr-local-agent-v" + MyAppVersion + "-windows-x64"
#define MyLogoFile "..\assets\goodhr-logo.ico"

[Setup]
AppId={{A7F8D98D-9D3D-47E7-A1F6-50F333A1F6D2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=GoodHR
DefaultDirName={code:GetDefaultInstallDir}
DefaultGroupName=GoodHR
DisableProgramGroupPage=yes
OutputDir=..\release
OutputBaseFilename=GoodHR-Local-Agent-Setup-v{#MyAppVersion}-windows-x64
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=no
RestartApplications=no
SetupIconFile={#MyLogoFile}
UninstallDisplayIcon={app}\goodhr-logo.ico
WizardSmallImageFile=..\assets\goodhr-logo-transparent-512.png

[Languages]
Name: "chinesesimplified"; MessagesFile: "ChineseSimplified.isl"

[Dirs]
Name: "{localappdata}\GoodHR\local-agent-new"

[InstallDelete]
; 升级前删除旧 Worker，避免新 Go 主程序混用旧 TypeScript 产物。
Type: filesandordirs; Name: "{app}\worker"

[Files]
Source: "{#MyPackageDir}\goodhr-local-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyPackageDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyPackageDir}\worker\*"; DestDir: "{app}\worker"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#MyLogoFile}"; DestDir: "{app}"; DestName: "goodhr-logo.ico"; Flags: ignoreversion

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："; Flags: checkedonce

[Icons]
Name: "{autoprograms}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\goodhr-logo.ico"
Name: "{autodesktop}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\goodhr-logo.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "启动 GoodHR Local Agent"; Flags: nowait postinstall skipifsilent

[Code]
// GetDefaultInstallDir 按 D、B、E、C 的顺序选择默认安装目录。
function GetDefaultInstallDir(Param: String): String;
begin
  if DirExists('D:\') then
    Result := 'D:\GoodHRLocalAgent'
  else if DirExists('B:\') then
    Result := 'B:\GoodHRLocalAgent'
  else if DirExists('E:\') then
    Result := 'E:\GoodHRLocalAgent'
  else
    Result := ExpandConstant('{localappdata}\Programs\GoodHRLocalAgent');
end;

// StopProcessTree 在复制文件前停止旧版主程序和它的 Worker 进程树。
procedure StopProcessTree(ImageName: String);
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{cmd}'), '/C taskkill /IM "' + ImageName + '" /T /F >NUL 2>NUL', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// StopGoodHRWorkerProcesses 只结束命令行指向当前安装目录的残留 Node Worker，避免锁住原生依赖导致升级回滚。
procedure StopGoodHRWorkerProcesses;
var
  ResultCode: Integer;
  Script: String;
  WorkerEntry: String;
begin
  WorkerEntry := ExpandConstant('{app}\worker\dist\main.js');
  Script := '$target = ''' + WorkerEntry + '''; ' +
    'Get-CimInstance Win32_Process -Filter "Name=''node.exe''" | ' +
    'Where-Object { $_.CommandLine -like (''*'' + $target + ''*'') } | ' +
    'ForEach-Object { Invoke-CimMethod -InputObject $_ -MethodName Terminate | Out-Null }';
  Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -Command "' + Script + '"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// PrepareToInstall 在复制文件前清理旧进程，避免原生依赖被残留 Worker 占用。
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopProcessTree('goodhr-local-agent.exe');
  StopGoodHRWorkerProcesses;
  Sleep(500);
  Result := '';
end;
