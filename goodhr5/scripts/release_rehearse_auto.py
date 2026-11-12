"""文件作用：自动发现并隔离验收本次镜像的新迁移，逐字段保护旧数据并验证完整旧应用回退。"""

import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import uuid

from release_activate import save_json
from release_cutover import CutoverError, require
from release_rehearse import Rehearsal, assert_preserved, main, migration_fingerprint
from release_rehearse_platforms import in_window
from release_rehearse_screening import ScreeningRehearsal


POLICY = json.loads(Path(__file__).with_name("auto-release-policy.json").read_text())
APPROVED = POLICY["bootstrap_migrations"]
LIBRARY = "system.auto_reply_library"


def check_archives(candidate, rollback, ledger, source):
    """check_archives 从干净源码和候选镜像自动冻结新增SQL，旧SQL逐字节不变，不依赖人工名单。"""
    require(ledger and rollback and set(rollback) <= set(ledger) <= set(candidate),
            "候选、回退与实际账本的迁移集合不符")
    require(all(candidate[name] == value for name, value in rollback.items()), "既有迁移SQL被修改")
    source_files = {path.name: path for path in source.glob("*.sql") if not path.name.endswith(".down.sql")}
    require(set(candidate) == set(source_files), "候选镜像没有完整携带本次源码迁移")
    for name, value in candidate.items():
        require(re.fullmatch(r"[0-9]{4}_[a-z0-9_]+\.sql", name) is not None
                and not source_files[name].is_symlink() and source_files[name].read_bytes() == value,
                "镜像内迁移与本次干净源码不一致：" + name)
    return {name: hashlib.sha256(candidate[name]).hexdigest() for name in sorted(set(candidate) - set(rollback))}


def check_ledger(before, after, start, end, migrations):
    """check_ledger 精确保留旧账本全部字段，只追加本次自动冻结的迁移并核对真实执行窗口。"""
    old = {row["filename"]: row for row in before}
    new = {row["filename"]: row for row in after}
    require(len(old) == len(before) and len(new) == len(after)
            and not set(old) & set(migrations)
            and set(new) == set(old) | set(migrations), "升级账本数量或集合不符")
    require(all(new[name] == row for name, row in old.items()), "旧迁移账本或原执行时间改变")
    for name in migrations:
        require(set(new[name]) == set(before[0]), "新增迁移账本字段异常")
        in_window(new[name]["applied_at"], start, end)


def check_library(before, after, sql, start, end):
    """check_library 独立核对唯一默认话术种子，保留自定义、原ID和创建时间，不忽略整张配置表。"""
    require(len(before) <= 1 and len(after) == 1, "默认话术记录缺失或重复")
    previous = json.loads(re.search(r"\$previous_library\$(.*?)\$previous_library\$", sql, re.S).group(1))
    latest = json.loads(re.search(r"\$new_library\$(.*?)\$new_library\$", sql, re.S).group(1))
    row = after[0]
    if not before:
        require(set(row) == {"id", "config_key", "config_value", "description", "enabled", "created_at", "updated_at"}
                and row["config_key"] == LIBRARY and row["config_value"] == latest
                and row["description"] == "岗位自动回复默认话术与可勾选的常用条件" and row["enabled"] is True,
                "默认话术新增记录不符合固定种子")
        uuid.UUID(row["id"])
        in_window(row["created_at"], start, end)
        in_window(row["updated_at"], start, end)
    elif before[0]["config_value"] == previous:
        expected = dict(before[0], config_value=latest, updated_at=row["updated_at"])
        require(row == expected, "内置话术升级改变了其他字段")
        in_window(row["updated_at"], start, end)
    else:
        require(row == before[0], "自定义或清空的默认话术被修改")


class AutomaticUpgradeRehearsal(ScreeningRehearsal):
    """复用备份、隔离网络、只读配对探测和回退登记，新增SQL自动按本次源码冻结后验收。"""

    migrations = ()

    def library_rows(self):
        """library_rows 仅在内存读取唯一种子记录，不把用户话术或其他配置值写入日志。"""
        return [json.loads(row) for row in self.psql(
            "SELECT row_to_json(t)::text FROM public.system_configs t "
            "WHERE config_key='system.auto_reply_library';").decode().splitlines()]

    def restore(self):
        """restore 在内部PG自动试跑本次新SQL，保护旧字段和账本，历史默认话术种子继续按原摘要单独核验。"""
        require(self.additive is None, "自动升级不得混用其他迁移例外")
        Rehearsal.restore(self)
        self.before_filenames, self.before_fingerprint = list(self.filenames), self.fingerprint
        self.before_ledger, self.before_schema = self.ledger_records(), self.database_schema()
        candidate = self.migration_archive(self.candidate["backend"], "candidate")
        rollback = self.migration_archive(self.rollback["backend"], "rollback")
        source = Path(self.args.source_root) / "cloud/backend/db/migrations"
        self.approved = check_archives(candidate, rollback, self.before_filenames, source)
        self.migrations = tuple(sorted(set(candidate) - set(self.before_filenames)))
        self.before_filenames = sorted(rollback)
        if not self.migrations:
            self.migrated_schema, self.migrated_ledger = self.before_schema, self.before_ledger
            self.psql("REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
            return
        library_migration = "0121_auto_reply_wechat_defaults.sql"
        library_upgrade = library_migration in self.migrations and self.approved[library_migration] == APPROVED[library_migration]
        columns = {name.replace(".", "\t", 1): [col["name"] for col in value["columns"]]
                   for name, value in self.before_schema["tables"].items() if name != "public.schema_migrations"}
        baseline = self.database_snapshot(columns, preserve_default_library=library_upgrade)
        before_library = self.library_rows() if library_upgrade else None
        statements = ["BEGIN;"]
        for name in self.migrations:
            statements.extend([candidate[name].decode(),
                               "INSERT INTO public.schema_migrations(filename) VALUES ('" + name + "');"])
        statements.append("COMMIT;")
        expected_schema = self.reference_schema(statements)
        start = self.psql("SELECT clock_timestamp()::text;").decode().strip()
        self.psql("\n".join(statements))
        end = self.psql("SELECT clock_timestamp()::text;").decode().strip()
        assert_preserved(baseline, self.database_snapshot(columns, preserve_default_library=library_upgrade))
        ledger, schema = self.ledger_records(), self.database_schema()
        check_ledger(self.before_ledger, ledger, start, end, self.migrations)
        if library_upgrade:
            check_library(before_library, self.library_rows(), candidate[library_migration].decode(), start, end)
        require(schema == expected_schema, "恢复库DDL与同一固定SQL的结构参考库不一致")
        self.psql("GRANT SELECT ON ALL TABLES IN SCHEMA public TO rehearsal_app; "
                  "REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        self.filenames, self.baseline = self.ledger(), self.database_snapshot()
        self.fingerprint = migration_fingerprint(self.filenames)
        self.migrated_schema, self.migrated_ledger = schema, ledger
        save_json(self.directory / "automatic-upgrade.json", {
            "approved_sha256": self.approved, "old_projected_data_preserved": True,
            "default_library_verified": library_upgrade, "old_ledger_preserved": True,
            "schema_verified": True, "execution": "fixed_image_sql_in_restored_database",
            "application_read_only": True, "before_fingerprint": self.before_fingerprint,
            "after_fingerprint": self.fingerprint,
        }, exclusive=True)


if __name__ == "__main__":
    try:
        sys.exit(main(AutomaticUpgradeRehearsal))
    except (CutoverError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
