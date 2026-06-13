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
Compression=lzma
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SetupIconFile=..\assets\icons\goodhr-logo.ico
UninstallDisplayIcon={app}\goodhr-logo.ico

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Dirs]
Name: "{app}\data"

[Files]
Source: "..\dist\installer-input\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\assets\icons\goodhr-logo.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\installer-input\worker-node\*"; DestDir: "{app}\worker-node"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--data-dir ""{app}\data"""; IconFilename: "{app}\goodhr-logo.ico"
Name: "{autodesktop}\GoodHR Local Agent"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--data-dir ""{app}\data"""; IconFilename: "{app}\goodhr-logo.ico"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Shortcuts:"; Flags: unchecked

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--data-dir ""{app}\data"""; Description: "Launch GoodHR Local Agent"; Flags: nowait postinstall
