# -*- mode: python ; coding: utf-8 -*-
"""GoodHR Local Agent 的 PyInstaller 打包配置。"""

import platform

from PyInstaller.utils.hooks import collect_data_files, collect_submodules


block_cipher = None

cloakbrowser_zip = "cloakbrowser_win.zip" if platform.system() == "Windows" else "cloakbrowser_mac.zip"

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
        (f"../vendor/downloads/{cloakbrowser_zip}", f"vendor/downloads/{cloakbrowser_zip}"),
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
    name="GoodHRLocalAgent",
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
)

coll = COLLECT(
    exe,
    a.binaries,
    a.zipfiles,
    a.datas,
    strip=False,
    upx=True,
    upx_exclude=[],
    name="GoodHRLocalAgent",
)

app = BUNDLE(
    coll,
    name="GoodHRLocalAgent.app",
    icon=None,
    bundle_identifier="cn.58it.goodhr.localagent",
)
