#!/usr/bin/env python3
"""文件作用：在 Windows 原生公开站点验收前准备隔离的 152 原版与窗口修补候选，核验双层归档；不发布或连接招聘网站。"""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath, PureWindowsPath
import shutil
import subprocess
import sys
import time
import urllib.request
import zipfile

SOURCE_NAME = "camoufox-152.0.4-beta.30-win.x86_64.zip"
CANDIDATE_NAME = "camoufox-152.0.4-beta.30-goodhr-window-r1-win.x86_64.zip"
SOURCE_SHA = "ea52a02fb1cfb1813ef6a326bea03fb2b650c9774143d953a94a27bfc8f10072"
SOURCE_SIZE = 493141328
SOURCE_URL = "https://github.com/daijro/camoufox/releases/download/v152.0.4-beta.30/" + SOURCE_NAME
RESOURCE = "chrome/juggler/content/TargetRegistry.js"
PATCHED_RESOURCE_SHA = "d41ff340529a47f3da99bba20892ad5f3ce95fee51da5c0929d8bc818645993b"
METADATA_FIELDS = ("date_time", "compress_type", "comment", "extra", "create_system", "create_version",
                   "extract_version", "flag_bits", "volume", "internal_attr", "external_attr")


def digest(path):
    """分块计算归档完整摘要，不把浏览器包一次读入内存。"""
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def download_source(path):
    """下载固定公开原包并验证既有生产摘要，失败不继续解压或覆盖正式组件。"""
    if not path.exists():
        temporary = path.with_suffix(".zip.part")
        started = time.monotonic()
        request = urllib.request.Request(SOURCE_URL,
                                         headers={"User-Agent": "GoodHR-public-regression/1"})
        try:
            with urllib.request.urlopen(request, timeout=60) as response, temporary.open("xb") as target:
                assert response.status == 200, "公开原包没有返回 HTTP 200"
                for chunk in iter(lambda: response.read(1024 * 1024), b""):
                    assert time.monotonic() - started < 1200, "原包下载超过二十分钟"
                    target.write(chunk)
            assert temporary.stat().st_size == SOURCE_SIZE and digest(temporary) == SOURCE_SHA, "原包完整性不符"
            temporary.rename(path)
        finally:
            temporary.unlink(missing_ok=True)
    assert path.stat().st_size == SOURCE_SIZE and digest(path) == SOURCE_SHA, "既有原包与生产摘要不符"


def extract_source(source, destination):
    """只把无重复、无链接、无跨目录成员的固定原包展开到新隔离目录。"""
    assert not destination.exists(), "隔离浏览器目录已存在，拒绝复用旧现场"
    with zipfile.ZipFile(source) as archive:
        names = archive.namelist()
        assert len(names) == len({name.casefold() for name in names}), "归档有重复或大小写冲突"
        for entry in archive.infolist():
            name = entry.filename
            assert "\\" not in name and not PurePosixPath(name).is_absolute() and not PureWindowsPath(name).drive
            assert ".." not in PurePosixPath(name).parts and ((entry.external_attr >> 16) & 0o170000) != 0o120000
            assert (destination / name).resolve().is_relative_to(destination), "归档成员越界"
        archive.extractall(destination)
    (destination / "version.json").write_text('{"version":"152.0.4","release":"beta.30"}', encoding="utf-8")


def attest(source, candidate):
    """独立完整核对所有成员与 PE 文件，仅允许根 omni 内唯一窗口管理资源变化。"""
    unchanged = []
    pe_count = 0
    with zipfile.ZipFile(source) as original, zipfile.ZipFile(candidate) as patched:
        assert original.namelist() == patched.namelist() and original.comment == patched.comment
        changed = []
        for entry in original.infolist():
            after = patched.getinfo(entry.filename)
            for field in METADATA_FIELDS:
                assert getattr(entry, field) == getattr(after, field), f"归档元数据改变：{entry.filename}/{field}"
            with original.open(entry) as left, patched.open(after) as right:
                before_sha = hashlib.file_digest(left, "sha256").hexdigest()
                after_sha = hashlib.file_digest(right, "sha256").hexdigest()
            if before_sha != after_sha:
                changed.append(entry.filename)
            else:
                unchanged.append({"name": entry.filename, "sha256": before_sha})
            if entry.filename.lower().endswith((".dll", ".exe")):
                assert before_sha == after_sha, "Windows 原生程序内容改变"
                pe_count += 1
        assert changed == ["omni.ja"], f"存在额外归档变化：{changed}"
        with zipfile.ZipFile(io.BytesIO(original.read("omni.ja"))) as old_inner, \
                zipfile.ZipFile(io.BytesIO(patched.read("omni.ja"))) as new_inner:
            assert old_inner.namelist() == new_inner.namelist() and old_inner.comment == new_inner.comment
            for entry in old_inner.infolist():
                after = new_inner.getinfo(entry.filename)
                for field in METADATA_FIELDS:
                    assert getattr(entry, field) == getattr(after, field), f"内层元数据改变：{entry.filename}/{field}"
            inner_changed = [name for name in old_inner.namelist()
                             if hashlib.sha256(old_inner.read(name)).digest() != hashlib.sha256(new_inner.read(name)).digest()]
            assert inner_changed == [RESOURCE], "内层存在窗口修补以外的变化"
            assert hashlib.sha256(new_inner.read(RESOURCE)).hexdigest() == PATCHED_RESOURCE_SHA
    return {"source_sha256": digest(source), "candidate_sha256": digest(candidate),
            "candidate_size": candidate.stat().st_size, "changed_outer_members": changed,
            "changed_inner_members": inner_changed, "unchanged_pe_count": pe_count,
            "unchanged_members": unchanged, "patched_resource_sha256": PATCHED_RESOURCE_SHA}


def main():
    """按固定路径准备基线与候选，并输出原生验收所需的完整可审计清单。"""
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--repo", required=True)
    arguments = parser.parse_args()
    root = Path(arguments.root).resolve()
    assert root.name == "camoufox-upgrade" and sys.platform == "win32", "此入口仅供原生 Windows 隔离验收"
    normalized = root.as_posix().lower() + "/"
    assert not any(item in normalized for item in ("/appdata/local/goodhr/", "/appdata/roaming/goodhr/"))
    script = Path(arguments.repo).resolve() / "goodhr5/local-agent-go-new/scripts/patch-camoufox-window-rendering.py"
    archives, results = root / "archives", root / "results"
    archives.mkdir(parents=True, exist_ok=True)
    results.mkdir(parents=True, exist_ok=True)
    source, candidate = archives / SOURCE_NAME, archives / CANDIDATE_NAME
    download_source(source)
    baseline = root / "browsers/152-baseline-win-x64"
    install = root / "browsers/152-candidate-installed-win-x64"
    extract_source(source, baseline)
    assert not install.exists()
    shutil.copytree(baseline, install)
    completed = subprocess.run([sys.executable, str(script), "--install-dir", str(install), "--output-dir", str(results),
                                "--source-zip", str(source), "--release-zip", str(candidate)],
                               text=True, capture_output=True, check=True)
    (results / "windows-release-manifest.json").write_text(completed.stdout, encoding="utf-8")
    report = attest(source, candidate)
    report.update({"platform": sys.platform, "python": sys.version, "root": str(root), "source_url": SOURCE_URL,
                   "baseline": str(baseline), "candidate_installation": str(install)})
    (results / "windows-independent-attestation.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({key: value for key, value in report.items() if key != "unchanged_members"}, ensure_ascii=False))


if __name__ == "__main__":
    main()
