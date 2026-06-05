; 本文件负责使用 Inno Setup 生成 GoodHR Local Agent Windows 安装器。

#define MyAppName "GoodHR"
#define MyAppVersion "5.1.0"
#define MyAppPublisher "GoodHR"
#define MyAppExeName "GoodHR.exe"

[Setup]
AppId={{7F89B944-A54D-47B8-9D75-8E58B1CF28C8}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={localappdata}\GoodHR
DisableProgramGroupPage=no
DefaultGroupName=GoodHR
AllowNoIcons=yes
OutputDir=..\dist-installer
OutputBaseFilename=GoodHRSetup-{#MyAppVersion}
Compression=lzma
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
PrivilegesRequired=lowest
SetupIconFile=..\assets\icons\goodhr-logo.ico
UninstallDisplayIcon={app}\{#MyAppExeName}

[Languages]
Name: "chinesesimp"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："; Flags: checkedonce

[Files]
Source: "..\dist\GoodHR\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\GoodHR"; Filename: "{app}\{#MyAppExeName}"
Name: "{commondesktop}\GoodHR"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "启动 GoodHR"; Flags: nowait postinstall skipifsilent
