#!/usr/bin/env python3
"""文件作用：仅给隔离 Camoufox 152 副本回移上游窗口刷新修复，保留归档备份与校验记录，不操作网页或发布配置。"""
import argparse
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import tempfile
import zipfile

RESOURCE = "chrome/juggler/content/TargetRegistry.js"
SOURCE_URL = "https://github.com/daijro/camoufox/pull/861"
MARKER = "      // GoodHR window rendering backport v1"
ANCHOR = ("      if (!domWindow.isChromeWindow)\n        return;\n"
          "      // In persistent mode, window might be opened long ago and might be\n")
INSERT = (MARKER + "\n      // 保持被遮挡窗口刷新，避免标准滚轮等待下一帧时停住。\n"
          f"      // 来源：{SOURCE_URL}\n"
          "      domWindow.browsingContext.forceAppWindowActive = true;\n")
REPLACEMENT = ANCHOR.replace("      // In persistent mode", INSERT + "      // In persistent mode")


def digest(file):
    """分块计算文件 SHA256，避免把整个浏览器归档读入内存。"""
    result = hashlib.sha256()
    with Path(file).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def patched_source(content):
    """仅替换唯一完整的打开窗口锚点，已正确安装时原样返回，未知结构立即拒绝。"""
    source = content.decode("utf-8")
    if MARKER in source:
        if source.count(MARKER) != 1 or source.count(REPLACEMENT) != 1:
            raise ValueError("已有窗口修复标记的结构无法确认，原资源不修改")
        return content
    if "forceAppWindowActive" in source or source.count(ANCHOR) != 1:
        raise ValueError("打开窗口锚点缺失、重复或已有未知修复，原资源不修改")
    return source.replace(ANCHOR, REPLACEMENT, 1).encode("utf-8")


def patch_browser(install_dir, output_dir):
    """验证隔离路径和 152 版本，原子更新单个归档资源，并把备份留在浏览器目录之外。"""
    root = Path(install_dir).resolve(strict=True)
    normalized = root.as_posix().lower()
    if not root.is_dir() or "camoufox-upgrade" not in normalized.split("/"):
        raise ValueError("只允许 camoufox-upgrade 目录内的隔离浏览器副本")
    if any(part in normalized + "/" for part in (
            "/library/application support/goodhr/", "/appdata/roaming/goodhr/", "/appdata/local/goodhr/")):
        raise ValueError("禁止修改用户正式运行组件")
    version = json.loads((root / "version.json").read_text())["version"]
    if not isinstance(version, str) or not version.startswith("152."):
        raise ValueError("仅支持已核对结构的 Firefox 152，其他版本不修改")
    archives = [file.resolve(strict=True) for file in (
        root / "omni.ja", root / "Camoufox.app/Contents/Resources/omni.ja") if file.is_file()]
    if len(archives) != 1 or not archives[0].is_relative_to(root):
        raise ValueError("浏览器归档缺失、存在歧义或软链接指向隔离目录外")
    archive = archives[0]
    output = Path(output_dir).resolve()
    if output.is_relative_to(root):
        raise ValueError("备份目录必须位于浏览器目录之外，避免混入发布包")
    before = digest(archive)
    with zipfile.ZipFile(archive) as reader:
        targets = [entry for entry in reader.infolist() if entry.filename == RESOURCE]
        if len(targets) != 1:
            raise ValueError("标准 TargetRegistry 资源缺失或重复，原归档不修改")
        original = reader.read(targets[0])
        updated = patched_source(original)
        changed = original != updated
        backup = None
        output.mkdir(parents=True, exist_ok=True)
        if changed:
            backup = output / f"omni-original-{before}.ja"
            if backup.exists():
                if backup.is_symlink() or digest(backup) != before:
                    raise ValueError("已有备份校验不一致，停止更新")
            else:
                with archive.open("rb") as source, backup.open("xb") as target:
                    shutil.copyfileobj(source, target)
                    target.flush()
                    os.fsync(target.fileno())
            if digest(backup) != before:
                raise ValueError("原归档备份未通过 SHA256 校验，停止更新")
            descriptor, temporary = tempfile.mkstemp(prefix=".goodhr-window-", suffix=".ja", dir=archive.parent)
            os.close(descriptor)
            try:
                with zipfile.ZipFile(temporary, "w") as writer:
                    writer.comment = reader.comment
                    for entry in reader.infolist():
                        info = copy.copy(entry)
                        with writer.open(info, "w") as target:
                            if entry.filename == RESOURCE:
                                target.write(updated)
                            else:
                                with reader.open(entry) as source:
                                    shutil.copyfileobj(source, target)
                with zipfile.ZipFile(temporary) as candidate:
                    if candidate.testzip() is not None:
                        raise ValueError("新归档完整性检查未通过，原归档保留")
                os.chmod(temporary, archive.stat().st_mode & 0o7777)
                with open(temporary, "r+b") as candidate:
                    os.fsync(candidate.fileno())
                if digest(archive) != before:
                    raise ValueError("浏览器归档在检查期间变化，停止覆盖")
                reader.close()
                os.replace(temporary, archive)
            finally:
                Path(temporary).unlink(missing_ok=True)
    report = {"source": SOURCE_URL, "firefox_version": version, "changed": changed,
              "archive": str(archive), "backup": str(backup) if backup else None,
              "archive_sha256_before": before, "archive_sha256_after": digest(archive),
              "resource": RESOURCE, "resource_sha256_before": hashlib.sha256(original).hexdigest(),
              "resource_sha256_after": hashlib.sha256(updated).hexdigest()}
    record = output / f"window-rendering-{before[:12]}-{report['archive_sha256_after'][:12]}.json"
    record.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    return report


def isolated_artifact(value, must_exist):
    """只接受隔离目录内的原包或候选包路径，拒绝覆盖源文件和正式运行组件。"""
    supplied = Path(value)
    if supplied.is_symlink():
        raise ValueError("归档路径不能是软链接")
    resolved = supplied.resolve(strict=must_exist)
    normalized = resolved.as_posix().lower()
    if "camoufox-upgrade" not in normalized.split("/") or any(part in normalized + "/" for part in (
            "/library/application support/goodhr/", "/appdata/roaming/goodhr/", "/appdata/local/goodhr/")):
        raise ValueError("原包与候选包都必须位于 camoufox-upgrade 隔离目录")
    return resolved


def nested_resource(outer, member):
    """流式展开原包中的 omni 供只读校验，大归档自动落入临时文件而不占满内存。"""
    with tempfile.SpooledTemporaryFile(max_size=8 * 1024 * 1024) as temporary:
        with outer.open(member) as stream:
            shutil.copyfileobj(stream, temporary)
        temporary.seek(0)
        with zipfile.ZipFile(temporary) as archive:
            targets = [entry for entry in archive.infolist() if entry.filename == RESOURCE]
            if len(targets) != 1:
                raise ValueError("原包中的 TargetRegistry 资源缺失或重复")
            return archive.read(targets[0])


def package_candidate(source_zip, release_zip, output_dir, patch_report):
    """流式重写 Mac 应用包或 Windows 平铺包，只替换对应 omni，核对启动文件并禁止覆盖不同产物。"""
    source = isolated_artifact(source_zip, True)
    release = isolated_artifact(release_zip, False)
    changed_archive = Path(patch_report["archive"]).resolve(strict=True)
    mac = changed_archive.as_posix().endswith("/Camoufox.app/Contents/Resources/omni.ja")
    install_root = changed_archive.parents[3] if mac else changed_archive.parent
    if not mac and (changed_archive.name != "omni.ja" or not (install_root / "camoufox.exe").is_file()):
        raise ValueError("只支持 Mac 应用包或含 camoufox.exe 的 Windows 根目录 omni")
    if source == release or release.is_relative_to(install_root):
        raise ValueError("候选包不能覆盖原包，也不能写入浏览器安装目录")
    before = digest(source)
    if digest(changed_archive) != patch_report["archive_sha256_after"]:
        raise ValueError("已修复的浏览器归档后来发生变化，停止打包")
    with zipfile.ZipFile(changed_archive) as inner:
        actual = inner.read(RESOURCE)
    if hashlib.sha256(actual).hexdigest() != patch_report["resource_sha256_after"]:
        raise ValueError("修复资源与校验记录不一致")
    with zipfile.ZipFile(source) as reader:
        matches = [entry for entry in reader.infolist() if
                   (entry.filename.endswith("Camoufox.app/Contents/Resources/omni.ja") if mac else entry.filename == "omni.ja")]
        if len(matches) != 1:
            raise ValueError("对应平台原包中的 omni 路径缺失或存在歧义")
        member = matches[0]
        original = nested_resource(reader, member)
        if (hashlib.sha256(original).hexdigest() != patch_report["resource_sha256_before"]
                and patched_source(original) != actual):
            raise ValueError("原包资源与补丁前来源不一致，不能混用浏览器样本")
        if patched_source(original) != actual:
            raise ValueError("原包资源无法仅通过本工具的窗口修复得到目标资源")
        prefix = member.filename[:-len("Contents/Resources/omni.ja")] if mac else ""
        binary_member = prefix + "Contents/MacOS/camoufox" if mac else "camoufox.exe"
        binary = changed_archive.parents[2] / "Contents/MacOS/camoufox" if mac else install_root / "camoufox.exe"
        with reader.open(binary_member) as stream:
            binary_sha = hashlib.file_digest(stream, "sha256").hexdigest()
        if digest(binary) != binary_sha:
            raise ValueError("原包与隔离副本的浏览器启动文件不同，可能混用了架构或原包")
        release.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temporary = tempfile.mkstemp(prefix=".goodhr-release-", suffix=".zip", dir=release.parent)
        os.close(descriptor)
        try:
            with zipfile.ZipFile(temporary, "w") as writer:
                writer.comment = reader.comment
                for entry in reader.infolist():
                    with writer.open(copy.copy(entry), "w") as target:
                        if entry.filename == member.filename:
                            with changed_archive.open("rb") as stream:
                                shutil.copyfileobj(stream, target)
                        else:
                            with reader.open(entry) as stream:
                                shutil.copyfileobj(stream, target)
            with zipfile.ZipFile(temporary) as candidate:
                if candidate.testzip() is not None:
                    raise ValueError("候选包完整性校验失败")
            if digest(source) != before or digest(changed_archive) != patch_report["archive_sha256_after"]:
                raise ValueError("原包或修复归档在打包期间发生变化")
            after = digest(temporary)
            os.chmod(temporary, source.stat().st_mode & 0o7777)
            with open(temporary, "r+b") as candidate:
                os.fsync(candidate.fileno())
            try:
                os.link(temporary, release)
            except FileExistsError:
                if not release.is_file() or digest(release) != after:
                    raise ValueError("候选路径已有不同文件，拒绝覆盖") from None
        finally:
            Path(temporary).unlink(missing_ok=True)
    report = {"source_zip": str(source), "release_zip": str(release), "source_zip_sha256": before,
              "release_zip_sha256": after, "members_changed": [member.filename],
              "original_resource_sha256": hashlib.sha256(original).hexdigest(),
              "patched_resource_sha256": hashlib.sha256(actual).hexdigest(), "binary_sha256": binary_sha}
    record = Path(output_dir).resolve() / f"release-window-rendering-{after[:12]}.json"
    record.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    return report


def self_test():
    """用双层临时 ZIP 验证唯一命中、幂等、拒绝歧义、原件保留与候选包只替换 omni。"""
    with tempfile.TemporaryDirectory(prefix="goodhr-window-patch-") as temporary:
        root = Path(temporary) / "camoufox-upgrade/browser"
        output = root.parent / "evidence"
        root.mkdir(parents=True)
        (root / "version.json").write_text('{"version":"152.0.4"}')
        original = ("const onOpenWindow = async (appWindow) => {\n" + ANCHOR + "};\n").encode()
        archive = root / "Camoufox.app/Contents/Resources/omni.ja"
        archive.parent.mkdir(parents=True)
        binary = root / "Camoufox.app/Contents/MacOS/camoufox"
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"isolated mac browser binary")
        extra = zipfile.ZipInfo("unchanged.bin", (2024, 2, 3, 4, 5, 6))
        extra.external_attr, extra.comment, extra.compress_type = 0o100644 << 16, b"member-comment", zipfile.ZIP_DEFLATED
        with zipfile.ZipFile(archive, "w") as writer:
            writer.comment = b"archive-comment"
            writer.writestr(RESOURCE, original)
            writer.writestr(extra, b"keep exactly")
        original_archive = archive.read_bytes()
        source_zip, release_zip = root.parent / "source.zip", root.parent / "release.zip"
        with zipfile.ZipFile(source_zip, "w") as writer:
            writer.comment = b"outer-comment"
            writer.writestr("Camoufox.app/Contents/Resources/omni.ja", original_archive)
            writer.writestr("Camoufox.app/Contents/MacOS/camoufox", binary.read_bytes())
            writer.writestr(extra, b"outer keep exactly")
        first = patch_browser(root, output)
        assert first["changed"] and Path(first["backup"]).read_bytes() == original_archive
        with zipfile.ZipFile(archive) as reader:
            assert reader.read(RESOURCE).replace(INSERT.encode(), b"") == original
            assert reader.read("unchanged.bin") == b"keep exactly" and reader.comment == b"archive-comment"
            kept = reader.getinfo("unchanged.bin")
            assert (kept.date_time, kept.external_attr, kept.comment, kept.compress_type) == (
                extra.date_time, extra.external_attr, extra.comment, extra.compress_type)
        after = archive.read_bytes()
        assert not patch_browser(root, output)["changed"] and archive.read_bytes() == after
        packaged = package_candidate(source_zip, release_zip, output, first)
        assert packaged["members_changed"] == ["Camoufox.app/Contents/Resources/omni.ja"]
        with zipfile.ZipFile(source_zip) as source, zipfile.ZipFile(release_zip) as candidate:
            assert source.namelist() == candidate.namelist() and candidate.comment == b"outer-comment"
            for item in source.infolist():
                kept = candidate.getinfo(item.filename)
                for field in ("date_time", "external_attr", "comment", "compress_type", "extra", "create_system"):
                    assert getattr(item, field) == getattr(kept, field)
                if item.filename.endswith("/omni.ja"):
                    with zipfile.ZipFile(io.BytesIO(candidate.read(item.filename))) as nested:
                        assert nested.read(RESOURCE) == original.replace(ANCHOR.encode(), REPLACEMENT.encode())
                else:
                    assert source.read(item) == candidate.read(item.filename)
        same = release_zip.read_bytes()
        assert package_candidate(source_zip, release_zip, output, first)["release_zip_sha256"] == digest(release_zip)
        assert release_zip.read_bytes() == same
        occupied = root.parent / "occupied.zip"
        occupied.write_bytes(b"unrelated artifact")
        try:
            package_candidate(source_zip, occupied, output, first)
        except ValueError:
            pass
        else:
            raise AssertionError("已有不同产物必须拒绝覆盖")
        assert occupied.read_bytes() == b"unrelated artifact"
        with zipfile.ZipFile(archive, "w") as writer:
            writer.writestr(RESOURCE, original + original)
        ambiguous = archive.read_bytes()
        try:
            patch_browser(root, output)
        except ValueError:
            pass
        else:
            raise AssertionError("双锚点必须拒绝")
        assert archive.read_bytes() == ambiguous
    windows_self_test()
    print("SELF_TEST_OK unique_anchor idempotent ambiguity_rejected originals_preserved metadata_preserved nested_zip release_idempotent overwrite_rejected windows_root_zip windows_pe_dll_preserved windows_wrong_binary_rejected")


def windows_self_test():
    """验证 Windows 根目录封装、PE/DLL 字节及属性保持、内层唯一修改、幂等和错误启动文件拒绝。"""
    with tempfile.TemporaryDirectory(prefix="goodhr-win-window-patch-") as temporary:
        root = Path(temporary) / "camoufox-upgrade/windows"
        root.mkdir(parents=True)
        (root / "version.json").write_text('{"version":"152.0.4"}')
        original = ("const onOpenWindow = async (appWindow) => {\n" + ANCHOR + "};\n").encode()
        archive = root / "omni.ja"
        with zipfile.ZipFile(archive, "w") as inner:
            inner.comment = b"windows-inner-comment"
            inner.writestr(RESOURCE, original)
            inner.writestr("unchanged.dat", b"windows browser resource")
        original_archive = archive.read_bytes()
        executable = b"MZ\x90\x00windows-exe-identity"
        (root / "camoufox.exe").write_bytes(executable)
        files = {"omni.ja": original_archive, "camoufox.exe": executable,
                 "mozglue.dll": b"MZ\x90\x00windows-dll-identity", "browser/omni.ja": b"separate browser resources",
                 "distribution/policies.json": b'{"keep":true}'}
        source_zip, release_zip = root.parent / "windows-source.zip", root.parent / "windows-candidate.zip"
        with zipfile.ZipFile(source_zip, "w") as outer:
            outer.comment = b"windows-outer-comment"
            for name, content in files.items():
                item = zipfile.ZipInfo(name, (2024, 3, 4, 5, 6, 8))
                item.create_system, item.external_attr, item.comment = 0, 0x20, b"windows-member"
                item.compress_type = zipfile.ZIP_DEFLATED
                outer.writestr(item, content)
        evidence = root.parent / "windows-evidence"
        patch = patch_browser(root, evidence)
        packed = package_candidate(source_zip, release_zip, evidence, patch)
        assert packed["members_changed"] == ["omni.ja"]
        assert packed["binary_sha256"] == hashlib.sha256(executable).hexdigest()
        with zipfile.ZipFile(source_zip) as source, zipfile.ZipFile(release_zip) as candidate:
            assert source.namelist() == candidate.namelist() and source.comment == candidate.comment
            for item in source.infolist():
                kept = candidate.getinfo(item.filename)
                for field in ("date_time", "external_attr", "internal_attr", "comment", "compress_type", "extra", "create_system"):
                    assert getattr(item, field) == getattr(kept, field)
                if item.filename == "omni.ja":
                    with zipfile.ZipFile(io.BytesIO(candidate.read(item.filename))) as inner:
                        assert inner.namelist() == [RESOURCE, "unchanged.dat"]
                        assert inner.read(RESOURCE) == original.replace(ANCHOR.encode(), REPLACEMENT.encode())
                        assert inner.read("unchanged.dat") == b"windows browser resource"
                        assert inner.comment == b"windows-inner-comment"
                else:
                    assert source.read(item.filename) == candidate.read(item.filename)
        before = release_zip.read_bytes()
        assert not patch_browser(root, evidence)["changed"]
        package_candidate(source_zip, release_zip, evidence, patch)
        assert release_zip.read_bytes() == before
        (root / "camoufox.exe").write_bytes(b"MZwrong-architecture-or-source")
        try:
            package_candidate(source_zip, root.parent / "wrong-source.zip", evidence, patch)
        except ValueError as error:
            assert "启动文件不同" in str(error)
        else:
            raise AssertionError("Windows 启动文件来源不同必须拒绝")


def main():
    """解析隔离副本与外部证据目录，默认不下载、启动浏览器或更新生产配置。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--install-dir")
    parser.add_argument("--output-dir")
    parser.add_argument("--source-zip")
    parser.add_argument("--release-zip")
    parser.add_argument("--self-test", action="store_true")
    arguments = parser.parse_args()
    if arguments.self_test:
        self_test()
    elif not arguments.install_dir or not arguments.output_dir:
        parser.error("必须提供 --install-dir 和浏览器目录外的 --output-dir")
    else:
        if bool(arguments.source_zip) != bool(arguments.release_zip):
            parser.error("--source-zip 和 --release-zip 必须一起提供")
        report = patch_browser(arguments.install_dir, arguments.output_dir)
        if arguments.source_zip:
            report["release"] = package_candidate(arguments.source_zip, arguments.release_zip, arguments.output_dir, report)
        print(json.dumps(report, ensure_ascii=False))


if __name__ == "__main__":
    main()
