"""文件作用：自动发现并隔离验收本次镜像的新迁移，逐字段保护旧数据并验证完整旧应用回退。"""

import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import uuid

from release_activate import save_json
from release_cutover import CutoverError, require
from release_rehearse import Rehearsal, assert_preserved, command, main, migration_fingerprint
from release_rehearse_platforms import in_window
from release_rehearse_screening import ScreeningRehearsal
from release_retention import digest_file


POLICY = json.loads(Path(__file__).with_name("auto-release-policy.json").read_text())
APPROVED = POLICY["bootstrap_migrations"]
LIBRARY = "system.auto_reply_library"
FACT_RETIREMENT = "0126_retire_system_stat_facts.sql"
FACT_TABLE = "public.system_stat_facts"


def without_retired_fact_data(toc):
    """without_retired_fact_data 仅省略已取消统计表的一项数据，其他表数据、表定义和索引完整保留。"""
    lines = toc.splitlines(keepends=True)
    matches = [index for index, line in enumerate(lines)
               if re.match(rb"^[0-9]+; [0-9]+ [0-9]+ TABLE DATA public system_stat_facts(?: |\r?$)", line)]
    require(len(matches) == 1, "完整备份必须有且只有一项原统计表数据，禁止扩大恢复省略范围")
    lines[matches[0]] = b"; user-authorized retired statistics data: " + lines[matches[0]]
    return b"".join(lines)


def archived_fact_snapshot(container, dump):
    """archived_fact_snapshot 从完整备份流式读出退役表COPY数据并校验可解压性、行数和摘要，不落盘或执行SQL。"""
    digest, rows, seen, reading, complete = hashlib.sha256(), 0, False, False, False
    with dump.open("rb") as source, tempfile.TemporaryFile() as errors:
        process = subprocess.Popen(["docker", "exec", "-i", container, "pg_restore", "--data-only", "--schema=public", "--table=system_stat_facts", "--file=-"],
                                   stdin=source, stdout=subprocess.PIPE, stderr=errors)
        try:
            for line in process.stdout:
                if line.startswith(b"COPY public.system_stat_facts (") and line.rstrip().endswith(b"FROM stdin;"):
                    require(not seen, "退役表归档存在多个COPY数据段")
                    seen, reading = True, True
                elif reading and line.rstrip(b"\r\n") == b"\\.":
                    reading, complete = False, True
                elif reading:
                    digest.update(line)
                    rows += 1
            require(process.wait(timeout=630) == 0 and seen and complete and not reading,
                    "退役统计表的完整备份数据无法完整读出，不继续迁移")
        finally:
            process.stdout.close()
            if process.poll() is None:
                process.kill()
                process.wait()
    return {"rows": rows, "sha256": digest.hexdigest(), "digest_format": "backup_COPY_data_sha256",
            "restored_into_isolated_database": False}


def verify_fact_retirement(before, after, snapshot, backup_sha256, migration_sha256):
    """verify_fact_retirement 只允许用户取消的重复统计表转为无存储视图；其他旧表仍逐字段完整核验。"""
    require(FACT_TABLE in before["tables"] and FACT_TABLE not in after["tables"]
            and after.get("views", {}).get(FACT_TABLE, {}).get("kind") == "v",
            "取消统计明细后必须保留无存储的旧版查询视图")
    require(not any("capture_system_stat" in name for name in after.get("functions", {}))
            and not any(name.startswith("system_stat_") for table in after["tables"].values()
                        for name in table.get("triggers", {})), "统计明细采集仍在运行")
    require(isinstance(snapshot, dict) and isinstance(snapshot.get("rows"), int) and snapshot["rows"] >= 0
            and all(re.fullmatch(r"[0-9a-f]{64}", value or "") for value in
                    (snapshot.get("sha256"), backup_sha256, migration_sha256)), "取消统计明细缺少完整备份及旧表摘要")
    return {"table": FACT_TABLE, "before": snapshot, "backup_sha256": backup_sha256,
            "migration_sha256": migration_sha256, "replacement": "non_materialized_view",
            "user_authorized_projection_retirement": True}


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

    def restore_data_options(self, dump):
        """restore_data_options 先冻结镜像SQL和真实账本；仅在本次退役迁移中跳过已完整归档的统计数据。"""
        candidate = self.migration_archive(self.candidate["backend"], "candidate")
        rollback = self.migration_archive(self.rollback["backend"], "rollback")
        ledger = command(["docker", "exec", self.production_pg["Id"], "psql", "-XqAt", "-v", "ON_ERROR_STOP=1",
                          "-U", self.args.postgres_user, "-d", self.args.database, "-c",
                          "SELECT filename FROM public.schema_migrations ORDER BY filename;"]).decode().splitlines()
        check_archives(candidate, rollback, ledger, Path(self.args.source_root) / "cloud/backend/db/migrations")
        self.restore_archives, self.restore_initial_ledger = (candidate, rollback), ledger
        if FACT_RETIREMENT not in set(candidate) - set(ledger):
            return []
        self.backed_up_retired_facts = archived_fact_snapshot(self.pg, dump)
        save_json(self.directory / "retired-statistics-backup.json", {
            "table": FACT_TABLE, "backup_sha256": digest_file(dump), **self.backed_up_retired_facts,
        }, exclusive=True)
        path = self.directory / "restore-without-retired-statistics.list"
        path.write_bytes(without_retired_fact_data((self.directory / "database-toc.txt").read_bytes()))
        path.chmod(0o600)
        target = "/tmp/goodhr-restore-without-retired-statistics.list"
        command(["docker", "cp", str(path), self.pg + ":" + target])
        return ["--use-list", target]

    def library_rows(self):
        """library_rows 仅在内存读取唯一种子记录，不把用户话术或其他配置值写入日志。"""
        return [json.loads(row) for row in self.psql(
            "SELECT row_to_json(t)::text FROM public.system_configs t "
            "WHERE config_key='system.auto_reply_library';").decode().splitlines()]

    def restore(self):
        """restore 在内部PG自动试跑本次新SQL，保护旧字段和账本，历史默认话术种子继续按原摘要单独核验。"""
        require(self.additive is None, "自动升级不得混用其他迁移例外")
        self.retired_projections = []
        Rehearsal.restore(self)
        self.before_filenames, self.before_fingerprint = list(self.filenames), self.fingerprint
        self.before_ledger, self.before_schema = self.ledger_records(), self.database_schema()
        if hasattr(self, "restore_archives"):
            candidate, rollback = self.restore_archives
            require(sorted(self.restore_initial_ledger) == sorted(self.before_filenames), "完整备份与真实迁移账本不一致")
        else:
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
        retirement = FACT_RETIREMENT in self.migrations
        if retirement:
            require(FACT_TABLE in self.before_schema["tables"] and FACT_TABLE.replace(".", "\t", 1) in self.baseline,
                    "取消统计明细缺少真实旧表基线")
            # 只有用户明确取消的冗余投影可退役；业务表、账本和其他统计数据仍完整比较。
            columns.pop(FACT_TABLE.replace(".", "\t", 1))
            if hasattr(self, "backed_up_retired_facts"):
                require(self.baseline[FACT_TABLE.replace(".", "\t", 1)]["rows"] == 0, "已取消统计数据被重新恢复到副本")
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
        retired_projections = []
        if retirement:
            retired_projections.append(verify_fact_retirement(
                self.before_schema, schema, getattr(self, "backed_up_retired_facts", self.baseline[FACT_TABLE.replace(".", "\t", 1)]),
                digest_file(self.directory / "database.dump"), self.approved[FACT_RETIREMENT]))
            save_json(self.directory / "retired-statistics-projection.json", retired_projections[0], exclusive=True)
        self.retired_projections = retired_projections
        self.psql("GRANT SELECT ON ALL TABLES IN SCHEMA public TO rehearsal_app; "
                  "REVOKE CREATE ON SCHEMA public FROM rehearsal_app;")
        self.filenames, self.baseline = self.ledger(), self.database_snapshot()
        self.fingerprint = migration_fingerprint(self.filenames)
        self.migrated_schema, self.migrated_ledger = schema, ledger
        save_json(self.directory / "automatic-upgrade.json", {
            "approved_sha256": self.approved, "old_projected_data_preserved": not bool(retired_projections),
            "all_business_data_preserved": True,
            "default_library_verified": library_upgrade, "old_ledger_preserved": True,
            "schema_verified": True, "execution": "fixed_image_sql_in_restored_database",
            "application_read_only": True, "before_fingerprint": self.before_fingerprint,
            "after_fingerprint": self.fingerprint,
            "retired_non_business_projections": retired_projections,
        }, exclusive=True)

    def pair(self, label, images):
        """pair 沿用完整组合验收，将明确取消的非业务投影及备份摘要写入两份回退兼容报告。"""
        super().pair(label, images)
        if self.retired_projections:
            self.reports[label]["retired_non_business_projections"] = self.retired_projections
            save_json(self.directory / (label + "-pair-summary.json"), self.reports[label])

    def finish(self):
        """finish 保留原发布证据和回退登记，并显式披露用户授权移除的统计投影，不能冒充保留该表数据。"""
        super().finish()
        if self.retired_projections:
            path = self.root / "isolated-validation.json"
            evidence = json.loads(path.read_text())
            evidence["retired_non_business_projections"] = self.retired_projections
            evidence["legacy_data_scope"] = "all_business_data_except_explicitly_retired_statistics_projection"
            save_json(path, evidence)


if __name__ == "__main__":
    try:
        sys.exit(main(AutomaticUpgradeRehearsal))
    except (CutoverError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"success": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
