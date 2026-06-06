; 文件作用：使用 Inno Setup 生成 GoodHR Go 本地程序 Windows 安装器。
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
Compression=lzma
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64

[Languages]
Name: "chinesesimp"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Dirs]
Name: "{app}\data"

[Files]
Source: "..\dist\installer-input\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--data-dir ""{app}\data"""
Name: "{autodesktop}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--data-dir ""{app}\data"""; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："; Flags: unchecked

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--data-dir ""{app}\data"""; Description: "启动 GoodHR Local Agent"; Flags: nowait postinstall skipifsilent
