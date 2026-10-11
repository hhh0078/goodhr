"""文件作用：使用已核对的源码与 Linux 二进制构建 AI 充值赠送后端镜像，不操作正式容器或数据库。"""

import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import socket
import subprocess
import tarfile
import time


ROOT = pathlib.Path("/opt/goodhr-release/ai-recharge-backend-20261012")
BASE_IMAGE = "sha256:f82fa9573811363b3bbeeab428e776371f3830ed9fa32bcd43815b3044750477"
CURRENT_BACKEND_IMAGE = "sha256:5257e7034c7b5f9ecd0877dfd06de80aa3d03ebcc14fe303af3ea2be054b954c"
EXPECTED_COMMIT = "0dfaf44ae1f91562ccb359684642a5e980a10e32"
EXPECTED_SOURCE_SHA256 = "19d1d84ae2939e139eb328172d6e1694888e0ea745bec2bf5e93430276289b66"
EXPECTED_BINARY_SHA256 = "06ec4dccc427e7a02e04b3a646ec5ed5ab4a146b274e991c8766153d5cbb22dc"


def digest(path):
    """流式计算发布输入摘要，避免把二进制整体读入内存。"""
    value = hashlib.sha256()
    with pathlib.Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def run(arguments, log_name=None, timeout=1200):
    """以参数数组执行固定构建命令，失败时只保留私有日志。"""
    if log_name:
        with (ROOT / log_name).open("xb") as output:
            result = subprocess.run(arguments, stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
        (ROOT / log_name).chmod(0o600)
    else:
        result = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        raise RuntimeError("后端构建操作未完成，请核对发布目录私有日志")
    return result.stdout


def extract_backend(archive, destination):
    """仅提取后端目录及祖先目录，拒绝链接、越界路径和异常体积。"""
    prefix = "goodhr5/cloud/backend"
    total = 0
    with tarfile.open(archive, "r:") as source:
        for member in source.getmembers():
            relative = pathlib.PurePosixPath(member.name)
            if relative.is_absolute() or ".." in relative.parts:
                raise RuntimeError("源码归档包含越界路径")
            allowed = member.name == prefix or member.name.startswith(prefix + "/") or (
                member.isdir() and prefix.startswith(member.name.rstrip("/") + "/"))
            if not allowed or not (member.isfile() or member.isdir()):
                raise RuntimeError("源码归档包含后端范围外文件或链接")
            target = destination.joinpath(*relative.parts)
            if os.path.commonpath([str(target.resolve()), str(destination.resolve())]) != str(destination.resolve()):
                raise RuntimeError("源码解压目标越界")
            if member.isdir():
                target.mkdir(parents=True, mode=0o700, exist_ok=True)
                continue
            total += member.size
            if total > 300 * 1024 * 1024:
                raise RuntimeError("后端源码归档体积异常")
            target.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
            with source.extractfile(member) as input_stream, target.open("xb") as output:
                shutil.copyfileobj(input_stream, output)
            target.chmod(0o700 if member.mode & 0o111 else 0o600)


def inspect_image(name):
    """读取精确镜像元数据并要求唯一结果。"""
    values = json.loads(run(["docker", "image", "inspect", name]))
    if not isinstance(values, list) or len(values) != 1 or values[0].get("Id") != name:
        raise RuntimeError("Docker 镜像身份不明确")
    return values[0]


def main():
    """核对提交、输入摘要和基础镜像后构建唯一后端镜像并保存证明。"""
    parser = argparse.ArgumentParser(description="构建 AI 充值赠送后端镜像")
    parser.add_argument("--commit", required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--source-sha256", required=True)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--binary-sha256", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit or ""):
        raise RuntimeError("必须提供完整目标提交")
    if (args.commit, args.source_sha256, args.binary_sha256) != (
            EXPECTED_COMMIT, EXPECTED_SOURCE_SHA256, EXPECTED_BINARY_SHA256):
        raise RuntimeError("目标提交或发布材料摘要不是本轮已审定值")
    if socket.gethostname() != "ldclouda31539":
        raise RuntimeError("构建服务器身份不匹配")
    os.umask(0o077)
    ROOT.mkdir(parents=True, mode=0o700, exist_ok=True)
    ROOT.chmod(0o700)
    if ROOT.is_symlink() or ROOT.resolve() != ROOT or os.stat(ROOT).st_dev != os.stat("/").st_dev:
        raise RuntimeError("发布目录必须是根盘固定目录")
    if any((ROOT / name).exists() for name in ("source.tar", "source", "goodhr-server", "backend-runtime", "backend-build-result.private.json")):
        raise RuntimeError("已有后端构建材料，禁止覆盖")
    source = pathlib.Path(args.source).resolve()
    binary = pathlib.Path(args.binary).resolve()
    if digest(source) != args.source_sha256 or digest(binary) != args.binary_sha256:
        raise RuntimeError("上传源码或二进制摘要不一致")
    base = inspect_image(BASE_IMAGE)
    current = inspect_image(CURRENT_BACKEND_IMAGE)
    if base["Config"].get("Cmd") != current["Config"].get("Cmd") or base["Config"].get("Entrypoint") != current["Config"].get("Entrypoint"):
        raise RuntimeError("后端基础镜像启动方式与当前镜像不一致")
    shutil.copyfile(source, ROOT / "source.tar")
    shutil.copyfile(binary, ROOT / "goodhr-server")
    (ROOT / "source.tar").chmod(0o600)
    (ROOT / "goodhr-server").chmod(0o700)
    source_root = ROOT / "source"
    source_root.mkdir(mode=0o700)
    extract_backend(ROOT / "source.tar", source_root)
    backend = source_root / "goodhr5/cloud/backend"
    migration = backend / "db/migrations/0092_ai_recharge_promotions.sql"
    if not (backend / "go.mod").is_file() or not migration.is_file():
        raise RuntimeError("后端源码缺少 go.mod 或 0092 迁移")
    runtime = ROOT / "backend-runtime"
    runtime.mkdir(mode=0o700)
    shutil.copyfile(ROOT / "goodhr-server", runtime / "goodhr-server")
    (runtime / "goodhr-server").chmod(0o700)
    shutil.copytree(backend / "db/migrations", runtime / "db/migrations")
    dockerfile = "\n".join([
        "FROM " + BASE_IMAGE,
        "LABEL org.opencontainers.image.revision=" + json.dumps(args.commit),
        "LABEL goodhr.release=" + json.dumps(ROOT.name),
        "COPY goodhr-server /usr/local/bin/goodhr-server",
        "COPY db/migrations /app/db/migrations",
        "",
    ])
    (runtime / "Dockerfile").write_text(dockerfile, encoding="utf-8")
    tag = "goodhr-production-backend:ai-recharge-20261012-" + args.commit[:8]
    started = time.monotonic()
    run(["docker", "build", "--network=none", "-t", tag, str(runtime)], "backend-image.private.log")
    image_values = json.loads(run(["docker", "image", "inspect", tag]))
    if not isinstance(image_values, list) or len(image_values) != 1:
        raise RuntimeError("构建镜像结果不唯一")
    image = image_values[0]
    labels = image["Config"].get("Labels") or {}
    if labels.get("org.opencontainers.image.revision") != args.commit or labels.get("goodhr.release") != ROOT.name:
        raise RuntimeError("构建镜像提交或发布标签不一致")
    if image["Config"].get("Cmd") != current["Config"].get("Cmd") or image["Config"].get("Entrypoint") != current["Config"].get("Entrypoint"):
        raise RuntimeError("新后端镜像启动方式变化")
    result = {
        "commit": args.commit,
        "source_sha256": args.source_sha256,
        "binary_sha256": args.binary_sha256,
        "migration_0092_sha256": digest(migration),
        "image_id": image["Id"],
        "image_tag": tag,
        "image_bytes": image["Size"],
        "build_seconds": round(time.monotonic() - started, 3),
    }
    temporary = ROOT / "backend-build-result.private.json.tmp"
    temporary.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    temporary.chmod(0o600)
    temporary.replace(ROOT / "backend-build-result.private.json")
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except Exception as cause:
        print(json.dumps({"success": False, "error": str(cause)}, ensure_ascii=False))
        raise SystemExit(1)
