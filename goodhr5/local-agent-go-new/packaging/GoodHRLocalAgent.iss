; 文件作用说明：使用 Inno Setup 生成 Windows x64 GoodHR 本地程序安装器。
#define MyAppName "GoodHR Local Agent"
#ifndef MyAppVersion
#define MyAppVersion "6"
#endif
#ifndef MyEnvironmentSuffix
#define MyEnvironmentSuffix ""
#endif
#ifndef MyPublicBaseURL
#define MyPublicBaseURL "https://goodhr.58it.cn"
#endif
#define MyAppExeName "goodhr-local-agent.exe"
#define MyPackageDir "..\release\goodhr-local-agent-v" + MyAppVersion + MyEnvironmentSuffix + "-windows-x64"
#define MyLogoFile "..\assets\goodhr-logo.ico"

[Setup]
AppId={{A7F8D98D-9D3D-47E7-A1F6-50F333A1F6D2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=GoodHR
DefaultDirName={code:GetDefaultInstallDir}
DefaultGroupName=GoodHR
DisableProgramGroupPage=yes
; 每次安装默认勾选桌面快捷方式，不沿用上一次安装的任务选择。
UsePreviousTasks=no
OutputDir=..\release
OutputBaseFilename=GoodHR-Local-Agent-Setup-v{#MyAppVersion}{#MyEnvironmentSuffix}-windows-x64
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
Name: "{app}\data"; Flags: uninsneveruninstall

[InstallDelete]
; 升级前删除旧 Worker，避免新 Go 主程序混用旧 TypeScript 产物。
Type: filesandordirs; Name: "{app}\worker"

[Files]
Source: "{#MyPackageDir}\goodhr-local-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyPackageDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyPackageDir}\build-info.json"; DestDir: "{app}"; Flags: ignoreversion
; 一并安装本地 Word 解析组件的许可证与版权说明。
Source: "{#MyPackageDir}\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#MyPackageDir}\worker\*"; DestDir: "{app}\worker"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#MyLogoFile}"; DestDir: "{app}"; DestName: "goodhr-logo.ico"; Flags: ignoreversion

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："

[Icons]
Name: "{autoprograms}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\goodhr-logo.ico"
Name: "{autodesktop}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\goodhr-logo.ico"; Tasks: desktopicon

[Run]
; 插件已在 ssPostInstall 阶段先同步；随后再启动主程序，避免两个模式争用同一可执行文件。
; postinstall 让手动安装完成页默认勾选立即运行；不使用 skipifsilent，保证自动更新结束后也会重新启动。
Filename: "{app}\{#MyAppExeName}"; Description: "安装完成后立即运行 GoodHR 小助手"; Flags: nowait postinstall

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

// ChromeExtensionDir 返回当前用户专用的 Chrome 插件稳定目录。
function ChromeExtensionDir: String;
begin
  Result := ExpandConstant('{localappdata}\GoodHRLocalAgent\browser-extension\chrome');
end;

// ChromeExtensionLoadMarker 返回插件首次加载或升级后需要重新加载的标记文件。
function ChromeExtensionLoadMarker: String;
begin
  Result := AddBackslash(ChromeExtensionDir) + '.goodhr-load-required';
end;

// FindChromeExecutable 查找当前机器常见的 Chrome 安装位置。
function FindChromeExecutable: String;
var
  Candidate: String;
begin
  Result := '';
  Candidate := ExpandConstant('{localappdata}\Google\Chrome\Application\chrome.exe');
  if FileExists(Candidate) then
  begin
    Result := Candidate;
    Exit;
  end;
  Candidate := ExpandConstant('{pf}\Google\Chrome\Application\chrome.exe');
  if FileExists(Candidate) then
  begin
    Result := Candidate;
    Exit;
  end;
  Candidate := ExpandConstant('{pf32}\Google\Chrome\Application\chrome.exe');
  if FileExists(Candidate) then
    Result := Candidate;
end;

// OpenChromeExtensionGuide 打开 Chrome 扩展管理页和已准备好的插件目录，并返回两个入口是否成功发起。
function OpenChromeExtensionGuide: Boolean;
var
  ChromePath: String;
  ResultCode: Integer;
  BrowserOpened: Boolean;
  FolderOpened: Boolean;
begin
  ChromePath := FindChromeExecutable;
  if ChromePath <> '' then
    BrowserOpened := Exec(ChromePath, 'chrome://extensions', '', SW_SHOWNORMAL, ewNoWait, ResultCode)
  else
    BrowserOpened := ShellExec('open', 'chrome://extensions', '', '', SW_SHOWNORMAL, ewNoWait, ResultCode);
  FolderOpened := ShellExec('open', ChromeExtensionDir, '', '', SW_SHOWNORMAL, ewNoWait, ResultCode);
  Result := BrowserOpened and FolderOpened;
end;

// PrepareLatestChromeExtension 调用已安装的新主程序下载、校验并准备最新版 Chrome 插件。
procedure PrepareLatestChromeExtension;
var
  ResultCode: Integer;
  Started: Boolean;
  LoadRequired: Boolean;
  Parameters: String;
begin
  ResultCode := -1;
  Parameters := '--prepare-browser-extension --browser-extension-dir "' + ChromeExtensionDir + '"';
  Started := Exec(ExpandConstant('{app}\{#MyAppExeName}'), Parameters, ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, ResultCode);
  if Started and (ResultCode = 0) then
  begin
    Log('GoodHR Chrome browser extension prepared successfully.');
    LoadRequired := FileExists(ChromeExtensionLoadMarker);
    if LoadRequired then
    begin
      if not WizardSilent then
        MsgBox('插件已经准备好了。Chrome 还需要你确认一下：' + #13#10 + #13#10 +
          '1. 打开右上角的“开发者模式”' + #13#10 +
          '2. 点击“加载已解压的扩展程序”' + #13#10 +
          '3. 选择接下来打开的插件文件夹' + #13#10 + #13#10 +
          '如果以前加载过，这次点一下“重新加载”就行。浏览器这一步必须由你点，我已经把文件准备好了。', mbInformation, MB_OK);
      if OpenChromeExtensionGuide then
      begin
        if not DeleteFile(ChromeExtensionLoadMarker) then
          Log('GoodHR Chrome browser extension load marker could not be cleared.');
      end
      else
        Log('GoodHR Chrome browser extension guide could not be opened; keeping the load marker.');
    end;
    if not LoadRequired then
      Log('GoodHR Chrome browser extension is current; skipped the repeated guide.');
    Exit;
  end;

  Log(Format('GoodHR Chrome browser extension preparation failed. started=%d exit_code=%d', [Ord(Started), ResultCode]));
  if not WizardSilent then
  begin
    if MsgBox('插件这次没准备成功，但本地程序已经装好了。要不要去官网下载页手动安装？', mbError, MB_YESNO) = IDYES then
      ShellExec('open', '{#MyPublicBaseURL}/download#browser-extension', '', '', SW_SHOWNORMAL, ewNoWait, ResultCode);
  end;
end;

// CurStepChanged 在文件复制完成后先同步插件，再进入主程序启动步骤。
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    PrepareLatestChromeExtension;
end;
