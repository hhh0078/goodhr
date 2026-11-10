"""文件作用：供宝塔每天06:00完整备份GoodHR正式数据库，仅轮换本任务校验通过的最近七份备份，保留发布与其他项目备份。"""

import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import socket
import sys
import urllib.parse

from release_cutover import CutoverError, require
from release_rehearse import command, dsn_database


ROOT = Path("/www/backup/database/goodhr-production-daily")
OWNER = "goodhr-daily-database-backup"
NAME = re.compile(r"goodhr-[0-9]{8}T[0-9]{12}Z-[0-9a-f]{8}\.dump\Z")
KEEP = 7


def file_sha(path):
    """file_sha 分块计算完整文件摘要，不将数据库正文载入内存或打印到日志。"""
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def safe_file(path):
    """safe_file 仅接受当前备份目录下本执行用户拥有的普通文件，拒绝链接和越界。"""
    require(path.parent == ROOT and path.is_file() and not path.is_symlink()
            and path.resolve() == path and path.stat().st_uid == os.geteuid(), "每日备份文件身份或路径不符")
    return path


def production_database():
    """production_database 核对实际运行后端的DSN和共享PostgreSQL端口，防止只按库名备份到其他数据库。"""
    backend, postgres = json.loads(command(["docker", "inspect", "goodhr-production-backend-1", "postgres"]))
    require(backend["State"]["Running"] and postgres["State"]["Running"], "正式后端或PostgreSQL未运行")
    environment = backend["Config"].get("Env", [])
    database = dsn_database(environment)
    dsn = [row.split("=", 1)[1] for row in environment if row.startswith("GOODHR_PG_DSN=")][-1]
    if dsn.startswith(("postgres://", "postgresql://")):
        value = urllib.parse.urlsplit(dsn)
        host, port = value.hostname, str(value.port or 5432)
    else:
        fields = dict(row.split("=", 1) for row in shlex.split(dsn) if "=" in row)
        host, port = fields.get("host", ""), fields.get("port", "5432")
    bindings = postgres["NetworkSettings"].get("Ports", {}).get("5432/tcp") or []
    require(backend["HostConfig"].get("NetworkMode") == "host" and host in {"127.0.0.1", "localhost"}
            and any(row.get("HostPort") == port and row.get("HostIp") in {"0.0.0.0", "127.0.0.1"} for row in bindings),
            "正式DSN未指向已核验的本机PostgreSQL，停止备份")
    return database, postgres["Id"]


def archive_toc(path, postgres):
    """archive_toc 使用同版本工具离线验证目录；无缓冲读取确保子进程从归档开头接收文件，不恢复或修改业务数据库。"""
    with safe_file(path).open("rb", buffering=0) as stream:
        require(stream.read(5) == b"PGDMP", "每日备份不是PostgreSQL自定义归档")
        stream.seek(0)
        toc = command(["docker", "exec", "-i", postgres, "pg_restore", "--list"], source=stream, timeout=60)
    require(b" TABLE " in toc, "每日备份目录中未找到数据表")
    return hashlib.sha256(toc).hexdigest()


def existing_backups(database, postgres):
    """existing_backups 只识别本任务同主机同数据库的完整清单，全部核验后才允许计算轮换范围。"""
    rows = []
    for manifest in sorted(ROOT.glob("goodhr-*.dump.manifest.json")):
        safe_file(manifest)
        value = json.loads(manifest.read_text(encoding="utf-8"))
        require(isinstance(value, dict), "每日备份清单格式异常，保留所有旧备份")
        if value.get("owner") != OWNER or value.get("database") != database:
            continue
        name = value.get("file", "")
        require(NAME.fullmatch(name) and manifest.name == name + ".manifest.json"
                and value.get("schema_version") == 1 and value.get("verified") is True
                and value.get("host") == socket.gethostname(), "每日备份清单身份异常，停止轮换")
        archive = safe_file(ROOT / name)
        require(archive.stat().st_size == value.get("size_bytes") and file_sha(archive) == value.get("sha256")
                and archive_toc(archive, postgres) == value.get("toc_sha256"), "已有每日备份校验未通过，保留所有备份")
        rows.append((name, archive, manifest))
    return sorted(rows)


def save_manifest(path, value):
    """save_manifest 独占创建私有清单并同步落盘，不覆盖任何既有备份或发布记录。"""
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def backup_once():
    """backup_once 新备份成功且全部七份保留候选校验通过后，仅删除本任务超出的旧归档及配对清单。"""
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(ROOT.resolve() == ROOT and not ROOT.is_symlink() and ROOT.stat().st_uid == os.geteuid(), "每日备份目录身份异常")
    os.chmod(ROOT, 0o700)
    lock_path = ROOT / ".backup.lock"
    descriptor = os.open(lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return {"success": True, "skipped": "上一轮每日备份仍在进行"}
        database, postgres = production_database()
        before = existing_backups(database, postgres)
        estimate = max([row[1].stat().st_size for row in before] or [0])
        require(shutil.disk_usage(ROOT).free >= 3 * 1024**3 + estimate + 256 * 1024**2,
                "每日备份可用空间不足，已保留所有旧备份及正式服务")
        name = "goodhr-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ") + "-" + secrets.token_hex(4) + ".dump"
        archive, partial = ROOT / name, ROOT / (name + ".partial")
        try:
            descriptor = os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, "wb") as stream:
                command(["docker", "exec", postgres, "sh", "-c", 'exec pg_dump -U "$POSTGRES_USER" -Fc "$1"', "sh", database],
                        output=stream, timeout=1800)
                stream.flush()
                os.fsync(stream.fileno())
            safe_file(partial)
            toc = archive_toc(partial, postgres)
            require(not archive.exists(), "每日备份文件名冲突，保留旧记录")
            partial.rename(archive)
            manifest = {"schema_version": 1, "owner": OWNER, "host": socket.gethostname(), "database": database,
                        "file": name, "size_bytes": archive.stat().st_size, "sha256": file_sha(archive),
                        "toc_sha256": toc, "verified": True, "created_at": dt.datetime.now(dt.timezone.utc).isoformat()}
            save_manifest(ROOT / (name + ".manifest.json"), manifest)
            after = existing_backups(database, postgres)
            require(any(row[0] == name for row in after), "新备份未通过轮换前核验")
            require(name in {row[0] for row in after[-KEEP:]}, "新备份时间早于保留范围，停止轮换并保留全部备份")
            removed = []
            for old_name, old_archive, old_manifest in after[:-KEEP]:
                # 再次核对本轮已校验的明确文件，备份目录只有本任务持锁写入。
                value = json.loads(safe_file(old_manifest).read_text(encoding="utf-8"))
                require(file_sha(safe_file(old_archive)) == value["sha256"], "轮换前备份变化，停止删除")
                old_archive.unlink()
                old_manifest.unlink()
                removed.append(old_name)
            result = {"success": True, "database": database, "backup": str(archive), "size_bytes": manifest["size_bytes"],
                      "sha256": manifest["sha256"], "kept": min(len(after), KEEP), "removed": removed}
            save_manifest(ROOT / (name + ".result.json"), result)
            return result
        finally:
            # 本轮半成品不能算成功备份；生成或校验失败时不轮换原有备份。
            if partial.exists():
                safe_file(partial).unlink()


def main():
    """main 输出不含连接密码的执行摘要；失败返回非零，只在全部校验通过后轮换旧备份。"""
    os.umask(0o077)
    try:
        print(json.dumps(backup_once(), ensure_ascii=False))
        return 0
    except (CutoverError, OSError, ValueError, KeyError, TypeError) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
