"""文件作用：按应用保留策略精确回收过期镜像导出包，保护全部数据库备份、源码及未知用途归档。"""

import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import re
import socket
import stat
import tarfile

from release_hygiene import (RELEASE_ROOT, SafetyReader, check_references, identity,
                            real_path, regular_file_sha)
from release_retention import (DockerRetention, POLICY_PATH, deployment_locks,
                               digest_file, read_json, require, save_json, verified_records)


EXCLUDED = {"source", "public", "downloads", "staged", "packages", "node_modules",
            "frontend-build", "frontend-runtime", "frontend-runner", "auto-deploy"}
SUFFIXES = (".tar", ".tar.gz", ".tgz")


def image_archive_ids(path):
    """只识别完整Docker镜像导出包；数据库、源码及无法证明用途的归档一律保留。"""
    try:
        with tarfile.open(path, "r:*") as archive:
            # 即使包内混有合法Docker manifest，也不能顺带删除数据库副本。
            if any(Path(item.name).name == "PG_VERSION" or "pgdata" in item.name.lower()
                   or item.name.lower().endswith((".dump", ".dump.gz", ".sql", ".sql.gz", ".backup", ".db", ".sqlite"))
                   for item in archive.getmembers()):
                return set()
            member = archive.getmember("manifest.json")
            if not member.isfile() or member.size > 1024 * 1024:
                return set()
            manifest = json.load(archive.extractfile(member))
            if not isinstance(manifest, list) or not manifest:
                return set()
            result = set()
            for row in manifest:
                config = row["Config"]
                if not re.fullmatch(r"(?:blobs/sha256/[0-9a-f]{64}|[0-9a-f]{64}\.json)", config):
                    return set()
                digest = Path(config).name.split(".")[0]
                config_member = archive.getmember(config)
                if not config_member.isfile() or config_member.size > 1024 * 1024:
                    return set()
                if hashlib.sha256(archive.extractfile(config_member).read()).hexdigest() != digest:
                    return set()
                result.add("sha256:" + digest)
            return result
    except (OSError, tarfile.TarError, ValueError, KeyError, TypeError, AttributeError):
        return set()


def protected_context(registry_path, reader):
    """核对真实线上和当前账本，按策略保护在用发布目录及全部有效兼容证据。"""
    registry, registry_sha = read_json(real_path(registry_path))
    snapshot = reader.snapshot()
    require(reader.migration_fingerprint(registry, snapshot) == registry["current_migration_fingerprint"], "迁移账本变化")
    live = {}
    for role in ("backend", "frontend"):
        rows = [x for x in snapshot["containers"] if x["Name"] == "/goodhr-production-" + role + "-1" and x["Running"]]
        require(len(rows) == 1, "正式运行容器不唯一")
        live[role] = rows[0]
    records, _ = verified_records(registry, snapshot["host"])
    require(any(all(row[role + "_image"] == live[role]["Image"] for role in live) for row in records),
            "当前线上缺少同账本的完整验收证据，保留全部归档")
    policy, _ = read_json(POLICY_PATH)
    rollback = None
    if policy.get("rollback_keep_per_service") == 1:
        rollback = next((x for x in records if all(x[role + "_image"] in snapshot["images"]
                        and x[role + "_image"] != live[role]["Image"] for role in live)), None)
        require(rollback is not None, "没有最近一套完整可用的兼容回退，保留全部归档")
    keep, cutoff = set(), []
    pairs = [{role: live[role]["Image"] for role in live}]
    if rollback:
        pairs.append({role: rollback[role + "_image"] for role in live})
    for images in pairs:
        tags = {snapshot["images"][key].get("ReleaseId") for key in images.values()}
        require(len(tags) == 1 and all(isinstance(x, str) and re.fullmatch(r"[A-Za-z0-9_.-]+", x) for x in tags), "保留镜像的完整版本编号不一致")
        keep.update(tags)
        cutoff.extend(dt.datetime.fromisoformat(snapshot["images"][key]["Created"][:19] + "+00:00").timestamp() for key in images.values())
    protected = []
    for name in sorted(keep):
        directory = real_path(RELEASE_ROOT / name, directory=True)
        dumps = [p for p in directory.glob("*.dump") if p.is_file()]
        require(dumps, "保留版本缺少数据库备份：" + name)
        for p in dumps:
            p = real_path(p)
            before = identity(p)
            protected.append({"path": str(p), "identity": before, "sha256": regular_file_sha(p, before)})
    for row in records:
        protected.append({"path": row["evidence_path"], "identity": identity(row["evidence_path"]), "sha256": row["evidence_sha256"]})
    return {"host": socket.gethostname(), "registry": str(registry_path), "registry_sha256": registry_sha,
            "snapshot": snapshot, "keep_releases": sorted(keep), "rollback_id": rollback["id"] if rollback else None,
            "cutoff": min(cutoff), "protected_files": protected}


def archive_candidates(root, keep, cutoff, protected_images=frozenset()):
    """只选择旧发布的无引用镜像导出包，数据库备份及未知用途归档不进入清单。"""
    rows = []
    for directory in sorted(root.iterdir()):
        if (not directory.is_dir() or directory.is_symlink() or directory.name in keep
                or directory.name in EXCLUDED or directory.name.startswith(("retained-upload-", "plugin-"))):
            continue
        if not re.search(r"20[0-9]{6}", directory.name):
            continue
        # 失败恢复库仍由原精确PG清理流程处理，并保留它依赖的备份。
        if any(directory.rglob("PG_VERSION")):
            continue
        for path in sorted(directory.rglob("*")):
            relative = path.relative_to(directory)
            if len(relative.parts) > 3 or EXCLUDED.intersection(relative.parts) or not path.name.endswith(SUFFIXES):
                continue
            value = path.lstat()
            if (not stat.S_ISREG(value.st_mode) or value.st_nlink != 1 or value.st_mtime >= cutoff
                    or path.resolve() != path or value.st_dev != root.stat().st_dev):
                continue
            before = identity(path)
            images = image_archive_ids(path)
            if not images or images.intersection(protected_images):
                continue
            rows.append({"path": str(path), "identity": before, "sha256": regular_file_sha(path, before),
                         "allocated_bytes": value.st_blocks * 512})
    return rows


def make_plan(registry, reader=None):
    """生成绑定线上快照、账本、保留备份和每个待删文件SHA的精确清单。"""
    policy, _ = read_json(POLICY_PATH)
    require(policy.get("delete_expired_release_archives") is True
            and policy.get("rollback_keep_per_service") in (0, 1)
            and policy.get("delete_original_database_backups") is False,
            "归档轮换必须保护全部数据库备份")
    root = real_path(RELEASE_ROOT, directory=True)
    context = protected_context(registry, reader or DockerRetention())
    keep = set(context["keep_releases"])
    snapshot = context["snapshot"]
    protected_images = {key for key, row in snapshot["images"].items() if row.get("ReleaseId") in keep}
    protected_images.update(row["Image"] for row in snapshot["containers"])
    rows = archive_candidates(root, keep, context["cutoff"], protected_images)
    check_references(root, [Path(x["path"]) for x in rows], SafetyReader())
    return {"schema_version": 1, "context": context, "files": rows,
            "allocated_bytes": sum(x["allocated_bytes"] for x in rows)}


def execute_plan(plan, registry, reader=None):
    """持发布锁重算整个清单并核对每个文件后逐项删除，任何变化都停止，不递归删除目录。"""
    with deployment_locks():
        require(make_plan(registry, reader) == plan, "发布身份、保留备份或归档清单已变化，停止清理")
        for row in plan["files"]:
            path = real_path(row["path"])
            require(identity(path) == row["identity"] and regular_file_sha(path, row["identity"]) == row["sha256"], "归档文件在删除前发生变化")
            path.unlink()


def main():
    """提供分开的计划和按SHA执行入口；只输出计数和磁盘字节，不输出备份内容。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "apply"))
    parser.add_argument("--registry", required=True)
    parser.add_argument("--plan", required=True)
    parser.add_argument("--sha256")
    args = parser.parse_args()
    if args.action == "plan":
        plan = make_plan(Path(args.registry))
        save_json(args.plan, plan)
    else:
        require(args.sha256 and digest_file(args.plan) == args.sha256, "清理计划SHA不符")
        plan, _ = read_json(args.plan)
        execute_plan(plan, Path(args.registry))
        save_json(args.plan + ".result.json", {"success": True, "files": len(plan["files"]),
                  "allocated_bytes": plan["allocated_bytes"], "keep_releases": plan["context"]["keep_releases"]})
    print(json.dumps({"action": args.action, "files": len(plan["files"]), "allocated_bytes": plan["allocated_bytes"]}))


if __name__ == "__main__":
    main()
