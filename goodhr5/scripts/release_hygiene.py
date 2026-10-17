"""文件作用：按已完成发布的显式清单回收构建缓存和已验证恢复副本，保留源码、备份、发布证据与线上服务。"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import socket
import stat
import subprocess
import sys

from release_guard import DockerReader, GuardError


RELEASE_ROOT = Path("/opt/goodhr-release")
MANIFEST_NAME = "release-hygiene-manifest.json"
CACHE_PATHS = frozenset({
    "source/goodhr5/cloud/frontend-next/node_modules", "source/goodhr5/cloud/frontend-next/.next",
    "frontend-build/node_modules", "frontend-build/.next",
    "frontend-runtime/node_modules", "frontend-runtime/.next",
    "frontend-runtime/standalone/node_modules", "frontend-runtime/standalone/.next",
    "frontend-runner/standalone/node_modules", "frontend-runner/standalone/.next",
    "backend-build/go-cache", "go-cache",
})
PG_PATHS = frozenset({"rehearsal-pgdata", "clone-pgdata-v2", "rehearsal-pgdata-before-readiness-fix", "pgdata"})
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
PROTECTED_NAMES = {MANIFEST_NAME, "configs-before.json", "configs-after.json", "manifest.private.json",
                   "windows-release-manifest.json", "deployment-result.private.json", "deploy-state.private.json", "rollback.sql"}


class HygieneError(ValueError):
    """表示清理保护条件没有满足，必须停止后续删除。"""


def require(condition, message):
    """require 在证据不足或路径不安全时立即停止。"""
    if not condition:
        raise HygieneError(message)


def file_sha(path):
    """file_sha 分块计算摘要，不读取或输出业务文件正文。"""
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def stat_identity(value):
    """stat_identity 统一提取路径或打开文件的身份及修改信息，不记录读取引起的访问时间。"""
    return [value.st_dev, value.st_ino, value.st_mode, value.st_uid, value.st_gid,
            value.st_size, value.st_mtime_ns, value.st_ctime_ns]


def identity(path):
    """identity 记录文件系统身份和修改信息，不记录访问时间。"""
    return stat_identity(Path(path).lstat())


def regular_file_sha(path, expected_identity):
    """regular_file_sha 不跟随链接流式计算普通文件内容SHA，并核对打开前后身份及读取期间变化。"""
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    digest = hashlib.sha256()
    try:
        descriptor = os.open(path, flags)
        with os.fdopen(descriptor, "rb") as stream:
            opened = os.fstat(stream.fileno())
            require(stat.S_ISREG(opened.st_mode) and stat_identity(opened) == expected_identity,
                    "缓存文件在打开前被替换或改变：" + str(path))
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
            require(stat_identity(os.fstat(stream.fileno())) == expected_identity,
                    "缓存文件在内容核验期间变化：" + str(path))
        require(identity(path) == expected_identity, "缓存文件路径在内容核验期间变化：" + str(path))
    except OSError:
        raise HygieneError("缓存文件无法安全读取：" + str(path)) from None
    return digest.hexdigest()


def real_path(path, directory=False):
    """real_path 拒绝目标及任意父级的符号链接、缺失路径和错误类型。"""
    path = Path(path).absolute()
    require(path.exists() and not path.is_symlink() and path.resolve() == path, "路径缺失或含符号链接：" + str(path))
    require(path.is_dir() if directory else path.is_file(), "路径类型不正确：" + str(path))
    return path


def release_path(path):
    """release_path 仅接受固定发布根目录下单个真实的一级发布目录。"""
    base = real_path(RELEASE_ROOT, directory=True)
    path = real_path(path, directory=True)
    require(path.parent == base, "只能处理/opt/goodhr-release下单个发布目录")
    return path


def relative_path(release, value):
    """relative_path 仅解析规范的相对路径，拒绝绝对路径、回退和Windows分隔符。"""
    require(isinstance(value, str) and value and "\\" not in value, "相对路径格式异常")
    part = PurePosixPath(value)
    require(not part.is_absolute() and ".." not in part.parts and str(part) == value and value != ".", "相对路径越界或不规范")
    return release.joinpath(*part.parts)


def load_manifest(release):
    """load_manifest 要求同一主机及已结束状态；失败发布仅可回收明确完成的可重建缓存。"""
    path = real_path(release / MANIFEST_NAME)
    value = json.loads(path.read_text(encoding="utf-8"))
    require(isinstance(value, dict), "发布manifest必须为对象")
    require(value.get("schema_version") == 1 and value.get("release_dir") == str(release)
            and value.get("host") == socket.gethostname(), "发布manifest身份不匹配")
    require(value.get("status") in {"succeeded", "failed"}, "发布尚未结束，保留现场")
    completed = value.get("completed_workdirs")
    require(isinstance(completed, list) and all(isinstance(x, str) for x in completed)
            and len(set(completed)) == len(completed), "已完成目录列表缺失或重复")
    require(all(x in CACHE_PATHS for x in completed), "manifest包含非白名单构建目录")
    require(isinstance(value.get("isolated_postgres", []), list), "隔离数据库资源列表格式异常")
    return value, file_sha(path)


def protected_file(path, target, kind):
    """protected_file 保护业务备份及发布证据；第三方包自身普通配置和SQL不视为业务文件。"""
    if kind == "isolated_postgres":
        return False
    name = path.name.lower()
    if name in PROTECTED_NAMES or name.endswith((".dump", ".dump.gz", ".sql.gz", ".backup")) or name.startswith("source.tar"):
        return True
    package_owned = False
    if target.name == "node_modules":
        parts = path.relative_to(target).parts
        depth = 2 if parts and parts[0].startswith("@") else 1
        package = target.joinpath(*parts[:depth]) / "package.json"
        package_owned = len(parts) > depth and package.is_file() and not package.is_symlink()
    return (name.endswith(".sql") or name == ".env" or name.startswith(".env.")) and not package_owned


def fingerprint(target, kind):
    """fingerprint 绑定全树元数据及每个普通文件的完整内容SHA，拒绝跨设备、特殊文件和业务备份。"""
    target = real_path(target, directory=True)
    device = target.stat().st_dev
    entries = [target]
    for current, directories, files in os.walk(target, followlinks=False):
        entries.extend(Path(current) / name for name in directories + files)
    digest, seen, allocated = hashlib.sha256(), set(), 0
    for path in sorted(set(entries)):
        value = path.lstat()
        require(value.st_dev == device, "目标内部存在跨设备挂载")
        require(stat.S_ISDIR(value.st_mode) or stat.S_ISREG(value.st_mode) or stat.S_ISLNK(value.st_mode), "缓存内部存在特殊文件")
        require(not protected_file(path, target, kind), "缓存内发现需保留的业务文件：" + str(path))
        before = stat_identity(value)
        link_target = os.readlink(path) if stat.S_ISLNK(value.st_mode) else ""
        content_sha = regular_file_sha(path, before) if stat.S_ISREG(value.st_mode) else None
        require(identity(path) == before, "缓存条目在指纹核验期间变化：" + str(path))
        record = [str(path.relative_to(target)), before, link_target, content_sha]
        digest.update(json.dumps(record, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
        key = (value.st_dev, value.st_ino)
        if key not in seen:
            allocated += getattr(value, "st_blocks", (value.st_size + 511) // 512) * 512
            seen.add(key)
    return {"algorithm": "sha256-content-v1", "sha256": digest.hexdigest(),
            "entries": len(set(entries)), "allocated_bytes": allocated}


def overlaps(left, right):
    """overlaps 判断明确路径是否相同或互为上下级。"""
    return left == right or left in right.parents or right in left.parents


class SafetyReader(DockerReader):
    """复用已有Docker只读查询，仅检查容器身份、发布标签、挂载和离线备份目录。"""

    def containers(self):
        """containers 查询全部运行及停止容器，明确不读取环境变量。"""
        ids = self.query(["ps", "--all", "--quiet", "--no-trunc"]).split()
        require(all(SHA256.fullmatch(value) for value in ids), "容器列表格式异常")
        if not ids:
            return []
        template = '{"id":{{json .Id}},"status":{{json .State.Status}},"mounts":{{json .Mounts}},"release_dir":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.release.dir")}}{{else}}null{{end}},"role":{{if .Config.Labels}}{{json (index .Config.Labels "goodhr.release.role")}}{{else}}null{{end}}}'
        rows = [json.loads(line) for line in self.query(["inspect", "--format", template] + ids).splitlines()]
        require(len(rows) == len(ids) and all(SHA256.fullmatch(row.get("id", "")) and isinstance(row.get("mounts"), list) for row in rows), "容器检查结果不完整")
        return rows

    def mounts(self):
        """mounts 只读取宿主挂载点，识别同一文件系统内的bind挂载。"""
        rows = []
        for line in Path("/proc/self/mountinfo").read_text().splitlines():
            value = re.sub(r"\\([0-7]{3})", lambda match: chr(int(match.group(1), 8)), line.split()[4])
            rows.append(Path(value))
        return rows

    def dump_toc(self, path):
        """dump_toc 离线读取dump目录；不连接业务库、不恢复数据、不新建容器。"""
        binary = shutil.which("pg_restore")
        command = [binary, "--list"] if binary else ["docker", "exec", "-i", "postgres", "pg_restore", "--list"]
        with path.open("rb") as stream:
            result = subprocess.run(command, stdin=stream, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
        require(result.returncode == 0 and b" TABLE " in result.stdout, "备份TOC不可读或缺少表对象")
        return hashlib.sha256(result.stdout).hexdigest()


def check_references(release, targets, reader):
    """check_references 核对所有容器挂载和本发布活动构建标签，不对受限proc字段作全量核验声明。"""
    containers = reader.containers()
    for row in containers:
        require(not (row["release_dir"] == str(release) and row["role"] in {"build", "builder", "restore", "rehearsal"}
                     and row["status"] not in {"exited", "dead"}), "本发布仍有活动构建或恢复容器")
        for mount in row["mounts"]:
            source = mount.get("Source", "")
            if isinstance(source, str) and Path(source).is_absolute():
                source = Path(source)
                require(not any(overlaps(source, target) or overlaps(source.resolve(), target) for target in targets), "清理目标仍被容器挂载引用")
    for mount in reader.mounts():
        require(not any(mount == target or target in mount.parents for target in targets), "目标内部存在活动挂载")
    return containers


def proof_file(release, value):
    """proof_file 核验本发布保留证据文件的原路径、大小、身份及约定摘要。"""
    require(isinstance(value, dict) and SHA256.fullmatch(value.get("sha256", "")), "证据摘要格式异常")
    path = real_path(relative_path(release, value.get("path")))
    before = identity(path)
    require(file_sha(path) == value["sha256"] and identity(path) == before, "保留证据文件变化")
    return {"path": str(path), "sha256": value["sha256"], "identity": before}


def candidate_specs(release, manifest, reader):
    """candidate_specs 仅生成manifest显式完成的白名单目标，历史无验证manifest的PG目录不自动清理。"""
    rows = [{"relative_path": value, "kind": "cache"} for value in manifest["completed_workdirs"]]
    containers = None
    for resource in manifest.get("isolated_postgres", []) if manifest["status"] == "succeeded" else []:
        require(isinstance(resource, dict) and resource.get("path") in PG_PATHS and resource.get("stopped") is True, "隔离PG资源尚未明确停止")
        ids = resource.get("container_ids")
        require(isinstance(ids, list) and ids and all(isinstance(value, str) and SHA256.fullmatch(value) for value in ids), "隔离PG资源缺少原容器身份")
        containers = reader.containers() if containers is None else containers
        require(not any(row["id"] in ids and row["status"] not in {"exited", "dead"} for row in containers), "隔离PG资源仍在运行")
        rehearsal = resource.get("rehearsal", {})
        require(isinstance(rehearsal, dict), "隔离恢复演练证据格式异常")
        require(rehearsal.get("passed") is True, "隔离恢复演练尚未通过")
        backup = proof_file(release, resource.get("backup"))
        report = proof_file(release, {"path": rehearsal.get("report_path"), "sha256": rehearsal.get("report_sha256")})
        with Path(backup["path"]).open("rb") as stream:
            require(stream.read(5) == b"PGDMP", "保留dump格式异常")
        backup["toc_sha256"] = reader.dump_toc(Path(backup["path"]))
        rows.append({"relative_path": resource["path"], "kind": "isolated_postgres", "backup": backup, "report": report})
    targets = [relative_path(release, row["relative_path"]) for row in rows]
    require(len(set(targets)) == len(targets) and not any(a != b and overlaps(a, b) for a in targets for b in targets), "目标重复或交叠")
    for row in rows:
        for key in ("backup", "report"):
            if key in row:
                require(not any(overlaps(Path(row[key]["path"]), target) for target in targets), "保留证据错误地位于清理树内")
    return rows


def create_plan(release_dir, reader=None):
    """create_plan 仅进行只读侦察和指纹计算，不修改服务器目录、容器或镜像。"""
    release = release_path(release_dir)
    reader = reader or SafetyReader()
    manifest, manifest_sha = load_manifest(release)
    specs = candidate_specs(release, manifest, reader)
    rows = []
    for spec in specs:
        target = relative_path(release, spec["relative_path"])
        require(not target.is_symlink(), "目标是符号链接")
        if target.exists():
            real_path(target, directory=True)
            if spec["kind"] == "isolated_postgres":
                real_path(target / "PG_VERSION")
            rows.append(dict(spec, path=str(target)))
    check_references(release, [Path(row["path"]) for row in rows], reader)
    for row in rows:
        row["fingerprint"] = fingerprint(Path(row["path"]), row["kind"])
    require(file_sha(release / MANIFEST_NAME) == manifest_sha, "计划期间发布manifest变化")
    return {"schema_version": 1, "purpose": "仅回收已完成发布的显式可重建目录；保留所有源码、备份、SQL和发布证据",
            "host": socket.gethostname(), "release_dir": str(release), "release_identity": identity(release)[:2],
            "manifest_sha256": manifest_sha, "candidates": rows,
            "visibility": "只核验全容器挂载、发布活动标签和宿主挂载；不声称读取全部受限proc的cwd/fd/maps"}


def write_json(path, value):
    """write_json 独占创建私有计划文件，禁止覆盖已有源码、配置或证据。"""
    with Path(path).open("x", encoding="utf-8") as stream:
        os.chmod(path, 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")


def apply_plan(plan_path, expected_sha, reader=None):
    """apply_plan 校验固定计划及全部保护后精确逐项删除；逐项记录日志，异常不继续。"""
    path = real_path(plan_path)
    require(isinstance(expected_sha, str) and SHA256.fullmatch(expected_sha) and file_sha(path) == expected_sha, "计划SHA不匹配")
    plan = json.loads(path.read_text(encoding="utf-8"))
    require(isinstance(plan, dict), "清理计划必须为对象")
    require(plan.get("schema_version") == 1 and plan.get("host") == socket.gethostname(), "计划主机或格式不匹配")
    release = release_path(plan.get("release_dir"))
    require(identity(release)[:2] == plan.get("release_identity"), "发布目录身份变化")
    require(file_sha(release / MANIFEST_NAME) == plan.get("manifest_sha256"), "发布manifest变化")
    reader = reader or SafetyReader()
    current = create_plan(release, reader)
    require(current == plan, "目标、保留证据或全树指纹已变化，停止清理")
    require(getattr(shutil.rmtree, "avoids_symlink_attacks", False), "本机不支持安全目录删除；只能在受支持的Linux主机执行")
    rows = plan["candidates"]
    targets = [Path(row["path"]) for row in rows]
    journal = path.with_name(path.name + ".apply.jsonl")
    require(not any(overlaps(path, target) or overlaps(journal, target) for target in targets), "计划或审计日志位于目标树内")
    result = {"purpose": "按批准SHA执行精确清理的私有审计", "plan_sha256": expected_sha, "deleted": [], "journal": str(journal)}
    with journal.open("x", encoding="utf-8") as stream:
        os.chmod(journal, 0o600)
        for index, row in enumerate(rows):
            target = Path(row["path"])
            check_references(release, targets[index:], reader)
            require(file_sha(path) == expected_sha and file_sha(release / MANIFEST_NAME) == plan["manifest_sha256"], "执行期间计划或manifest变化")
            require(fingerprint(target, row["kind"]) == row["fingerprint"], "删除前全树指纹变化")
            for item in rows:
                for key in ("backup", "report"):
                    if key in item:
                        evidence = item[key]
                        require(identity(real_path(evidence["path"])) == evidence["identity"], "保留证据身份或修改信息变化")
            stream.write(json.dumps({"attempting": str(target)}, ensure_ascii=False) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
            shutil.rmtree(target)
            require(not target.exists() and not target.is_symlink(), "目标未完整删除")
            result["deleted"].append(str(target))
            stream.write(json.dumps({"deleted": str(target)}, ensure_ascii=False) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        for item in rows:
            for key in ("backup", "report"):
                if key in item:
                    require(file_sha(item[key]["path"]) == item[key]["sha256"], "清理后保留证据摘要变化")
        result["success"] = True
        stream.write(json.dumps(result, ensure_ascii=False) + "\n")
    return result


def main(argv=None):
    """main 提供先生成计划、再按固定摘要执行的入口，不包含任何Docker删除或广域prune。"""
    parser = argparse.ArgumentParser(description="GoodHR发布完成后的精确缓存收尾")
    operations = parser.add_subparsers(dest="operation", required=True)
    plan_command = operations.add_parser("plan")
    plan_command.add_argument("--release-dir", required=True)
    plan_command.add_argument("--out", required=True)
    apply_command = operations.add_parser("apply")
    apply_command.add_argument("--plan", required=True)
    apply_command.add_argument("--sha256", required=True)
    args = parser.parse_args(argv)
    try:
        if args.operation == "plan":
            value = create_plan(args.release_dir)
            output = Path(args.out).absolute()
            require(output.parent.resolve() == output.parent and not any(overlaps(output, Path(row["path"])) for row in value["candidates"]), "计划输出路径不安全")
            write_json(output, value)
            result = {"plan": str(output), "sha256": file_sha(output), "targets": len(value["candidates"])}
        else:
            result = apply_plan(args.plan, args.sha256)
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except (HygieneError, GuardError, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
