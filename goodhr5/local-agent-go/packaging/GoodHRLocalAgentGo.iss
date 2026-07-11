; Purpose: build the GoodHR Go Local Agent Windows installer with Inno Setup.
#define MyAppName "GoodHR Local Agent"
#ifndef MyAppVersion
#define MyAppVersion "0.1.0"
#endif
#define MyAppPublisher "GoodHR"
#define MyAppExeName "goodhr-local-agent.exe"

[Setup]
AppId={{A7F8D98D-9D3D-47E7-A1F6-50F333A1F6D2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={localappdata}\Programs\GoodHRLocalAgent
DefaultGroupName=GoodHR
DisableProgramGroupPage=yes
OutputDir=..\dist-installer
OutputBaseFilename=GoodHRLocalAgentGoSetup-{#MyAppVersion}
Compression=zip
SolidCompression=no
WizardStyle=modern
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SetupIconFile=..\assets\icons\goodhr-logo.ico
UninstallDisplayIcon={app}\goodhr-logo.ico
CloseApplications=no
RestartApplications=no

[Languages]
Name: "chinesesimplified"; MessagesFile: ".\ChineseSimplified.isl"

[Dirs]
Name: "{app}\data"

[Files]
Source: "..\dist\installer-input\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\assets\icons\goodhr-logo.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\audio\*"; DestDir: "{app}\audio"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\dist\installer-input\worker-node\*"; DestDir: "{app}\worker-node"; Flags: ignoreversion recursesubdirs createallsubdirs
; 暂时不把 frontend-next 打进本地程序安装包，避免前端包影响本地程序打包。
; Source: "..\dist\installer-input\console\*"; DestDir: "{app}\data\console"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--restart --data-dir ""{app}\data"""; IconFilename: "{app}\goodhr-logo.ico"
Name: "{autodesktop}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--restart --data-dir ""{app}\data"""; IconFilename: "{app}\goodhr-logo.ico"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式（请务必勾选）"; GroupDescription: "快捷方式："

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--restart --data-dir ""{app}\data"""; Description: "启动 GoodHR Local Agent"; Flags: nowait postinstall

[Code]
// StopProcessByImageName 静默结束指定进程，避免升级安装时文件被旧程序占用。
procedure StopProcessByImageName(ImageName: String);
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{cmd}'), '/C taskkill /IM "' + ImageName + '" /T /F >NUL 2>NUL', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// CurStepChanged 在安装文件复制前清理旧进程，避免出现占用文件弹窗。
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
  begin
    StopProcessByImageName('goodhr-local-agent.exe');
    StopProcessByImageName('XtaCache.exe');
    StopProcessByImageName('XtaCache');
  end;
end;
