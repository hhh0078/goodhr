; 文件作用说明：使用 Inno Setup 生成 Windows x64 GoodHR 本地程序安装器。
#define MyAppName "GoodHR Local Agent"
#ifndef MyAppVersion
#define MyAppVersion "6"
#endif
#define MyAppExeName "goodhr-local-agent.exe"
#define MyPackageDir "..\release\goodhr-local-agent-v" + MyAppVersion + "-windows-x64"

[Setup]
AppId={{A7F8D98D-9D3D-47E7-A1F6-50F333A1F6D2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=GoodHR
DefaultDirName={localappdata}\Programs\GoodHRLocalAgent
DefaultGroupName=GoodHR
DisableProgramGroupPage=yes
OutputDir=..\release
OutputBaseFilename=GoodHRLocalAgentSetup-{#MyAppVersion}-windows-x64
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=no
RestartApplications=no

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Dirs]
Name: "{localappdata}\GoodHR\local-agent-new"

[InstallDelete]
; 升级前删除旧 Worker，避免新 Go 主程序混用旧 TypeScript 产物。
Type: filesandordirs; Name: "{app}\worker"

[Files]
Source: "{#MyPackageDir}\goodhr-local-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyPackageDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyPackageDir}\worker\*"; DestDir: "{app}\worker"; Flags: ignoreversion recursesubdirs createallsubdirs

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："; Flags: checkedonce

[Icons]
Name: "{autoprograms}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "启动 GoodHR Local Agent"; Flags: nowait postinstall skipifsilent

[Code]
// StopProcessTree 在复制文件前停止旧版主程序和它的 Worker 进程树。
procedure StopProcessTree(ImageName: String);
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{cmd}'), '/C taskkill /IM "' + ImageName + '" /T /F >NUL 2>NUL', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// CurStepChanged 在安装阶段清理旧进程，避免升级文件被占用。
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
  begin
    StopProcessTree('goodhr-local-agent.exe');
  end;
end;
