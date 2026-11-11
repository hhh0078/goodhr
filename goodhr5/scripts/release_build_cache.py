"""文件作用：按GoodHR专属编译标识和缓存依赖图生成精确回收计划，保护共享缓存、其他项目及全部运行镜像，不执行全局prune。"""

import argparse
import datetime as dt
import json
from pathlib import Path
import re
import socket
import urllib.parse

from release_activate import DockerEngine
from release_retention import DockerRetention, digest_file, read_json, save_json, GuardError, require


def normalize(rows):
    """统一Docker 26父缓存字段的历史空格，拒绝重复ID、模糊父链和无效状态。"""
    result = {}
    for row in rows:
        key = row.get("ID")
        require(isinstance(key, str) and re.fullmatch(r"[a-z0-9]{20,64}", key) and key not in result, "缓存ID缺失或重复")
        parents = row.get("Parents", row.get(" Parents", [])) or []
        require(isinstance(parents, list) and all(isinstance(value, str) for value in parents), "缓存父链不明确")
        require(type(row.get("InUse")) is bool and type(row.get("Shared")) is bool and isinstance(row.get("Description"), str), "缓存状态不明确")
        result[key] = {**row, "Parents": sorted(parents)}
        result[key].pop(" Parents", None)
    require(all(parent in result for row in result.values() for parent in row["Parents"]), "缓存父链缺项")
    return result


def closure(seeds, rows):
    """沿完整父链计算保护范围，不把共享Go基础误认成GoodHR独占。"""
    result, pending = set(seeds), list(seeds)
    while pending:
        key = pending.pop()
        for parent in rows[key]["Parents"]:
            if parent not in result:
                result.add(parent)
                pending.append(parent)
    return result


def targets(rows):
    """只接受明确生成goodhr-server的编译分支，其他项目及共享或在用记录的祖先始终保留。"""
    markers = {
        'mount / from exec /bin/sh -c CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /goodhr-server ./cmd/server',
        '[stage-1 4/6] COPY --from=builder /goodhr-server /usr/local/bin/goodhr-server',
    }
    continuations = {
        '[stage-1 5/6] COPY --from=builder /src/db ./db',
        '[stage-1 6/6] COPY --from=builder /src/templates ./templates',
    }
    owned = {key for key, row in rows.items() if row["Description"] in markers}
    while True:
        expanded = owned | {key for key, row in rows.items() if row["Description"] in continuations and set(row["Parents"]) & owned}
        if expanded == owned:
            break
        owned = expanded
    possible = closure(owned, rows)
    protected = closure((set(rows) - possible) | {key for key, row in rows.items() if row["InUse"] or row["Shared"]}, rows)
    remaining, ordered = possible - protected, []
    while remaining:
        parents = {parent for key in remaining for parent in rows[key]["Parents"]}
        leaves = sorted(remaining - parents)
        require(leaves, "缓存父链出现循环")
        ordered.extend(leaves)
        remaining.difference_update(leaves)
    return ordered


class CacheReader:
    """仅读取缓存元数据，并允许按单个完整缓存ID调用Docker受限回收接口。"""

    def __init__(self):
        """复用已有Docker连接与全局安全快照，不读取业务数据。"""
        self.engine, self.docker = DockerEngine(), DockerRetention()

    def caches(self):
        """读取全部缓存，保留外部项目以便校验它们没有被删除。"""
        return normalize(self.engine.request("GET", self.engine.prefix + "/system/df").get("BuildCache") or [])

    def remove(self, key):
        """只允许精确ID筛选，禁止无筛选回收或拼接任意过滤条件。"""
        require(re.fullmatch(r"[a-z0-9]{20,64}", key), "缓存ID不合法")
        query = urllib.parse.urlencode({"all": "true", "filters": json.dumps({"id": [key]})})
        return self.engine.request("POST", self.engine.prefix + "/build/prune?" + query)


def execute(plan, reader, audit_path):
    """每一步都重查完整缓存及容器镜像快照，只删除已批准ID，异常立即停止。"""
    expected = dict(plan["caches"])
    require(targets(expected) == plan["targets"], "计划与当前缓存归属规则不一致")
    audit = {"success": False, "completed": [], "space_reclaimed": 0}
    try:
        for key in plan["targets"]:
            require(reader.caches() == expected and reader.docker.snapshot() == plan["docker"], "删除前Docker状态变化，必须重新生成计划")
            audit["attempting"] = key
            save_json(audit_path, audit)
            response = reader.remove(key)
            deleted = response.get("CachesDeleted") or []
            require(deleted == [key], "Docker没有只删除本次批准的缓存ID，已停止")
            del expected[key]
            require(reader.caches() == expected and reader.docker.snapshot() == plan["docker"], "删除后有额外状态变化，已停止")
            audit["completed"].append(key)
            audit["space_reclaimed"] += response.get("SpaceReclaimed", 0)
            save_json(audit_path, audit)
        audit.update(success=True, attempting=None)
        save_json(audit_path, audit)
        return audit
    except Exception as error:
        audit["error"] = str(error)
        save_json(audit_path, audit)
        raise


def main():
    """先保存精确计划，再通过固定SHA执行；持有现有项目发布锁而不暂停服务。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("plan", "apply"))
    parser.add_argument("--source-root", required=True)
    parser.add_argument("--file", required=True)
    parser.add_argument("--sha256")
    args = parser.parse_args()
    source = Path(args.source_root)
    require(source.resolve() == source and source.name == "goodhr5", "项目源码路径不明确")
    import fcntl
    import os
    from release_rehearse import rehearsal_locks
    job = os.open("/opt/goodhr-release/auto-deploy/job.lock", os.O_RDWR | os.O_NOFOLLOW)
    try:
        fcntl.flock(job, fcntl.LOCK_SH | fcntl.LOCK_NB)
        with rehearsal_locks(source):
            reader = CacheReader()
            if args.operation == "plan":
                require(not Path(args.file).exists(), "禁止覆盖旧回收计划")
                caches, docker = reader.caches(), reader.docker.snapshot()
                require(reader.caches() == caches, "读取期间缓存状态变化")
                chosen = targets(caches)
                plan = {"schema_version": 1, "host": socket.gethostname(), "caches": caches, "docker": docker,
                        "targets": chosen, "code_sha256": digest_file(__file__)}
                save_json(args.file, plan)
                print(json.dumps({"plan": args.file, "sha256": digest_file(args.file), "targets": len(chosen),
                                  "estimated_bytes": sum(caches[key]["Size"] for key in chosen)}))
            else:
                plan, sha = read_json(args.file)
                require(sha == args.sha256 and plan["host"] == socket.gethostname()
                        and plan["code_sha256"] == digest_file(__file__), "计划SHA、主机或工具代码变化")
                audit = Path(args.file + ".apply-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ") + ".json")
                result = execute(plan, reader, audit)
                print(json.dumps({**result, "audit": str(audit)}))
    finally:
        os.close(job)


if __name__ == "__main__":
    main()
