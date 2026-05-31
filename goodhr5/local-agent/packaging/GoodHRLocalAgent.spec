# -*- mode: python ; coding: utf-8 -*-
"""GoodHR Local Agent 的 PyInstaller 打包配置。"""

import platform

from PyInstaller.utils.hooks import collect_data_files, collect_submodules


block_cipher = None
app_name = "GoodHR招聘助手"

cloakbrowser_zip = "cloakbrowser_win.zip" if platform.system() == "Windows" else "cloakbrowser_mac.zip"
app_icon = "../assets/icons/goodhr-logo.ico" if platform.system() == "Windows" else "../assets/icons/goodhr-logo.icns"

hiddenimports = (
    collect_submodules("app")
    + collect_submodules("rapidocr")
    + [
        "uvicorn",
        "uvicorn.lifespan.on",
        "uvicorn.loops.auto",
        "uvicorn.protocols.http.auto",
        "uvicorn.protocols.websockets.auto",
    ]
)

datas = (
    collect_data_files("rapidocr")
    + [
        (f"../vendor/downloads/{cloakbrowser_zip}", "vendor/downloads"),
        ("../assets", "assets"),
        ("../pyproject.toml", "."),
    ]
)

a = Analysis(
    ["../launcher.py"],
    pathex=[".."],
    binaries=[],
    datas=datas,
    hiddenimports=hiddenimports,
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=["paddleocr", "paddlepaddle", "paddlex"],
    win_no_prefer_redirects=False,
    win_private_assemblies=False,
    cipher=block_cipher,
    noarchive=False,
)
pyz = PYZ(a.pure, a.zipped_data, cipher=block_cipher)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name=app_name,
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    console=False,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
    icon=app_icon,
)

coll = COLLECT(
    exe,
    a.binaries,
    a.zipfiles,
    a.datas,
    strip=False,
    upx=True,
    upx_exclude=[],
    name=app_name,
)

app = BUNDLE(
    coll,
    name=f"{app_name}.app",
    icon=app_icon,
    bundle_identifier="cn.58it.goodhr.localagent",
)
