"""文件作用：离线验证五份筛选发布迁移的归档、旧业务数据与账本保护及DDL白名单；不访问Docker、数据库或生产。"""

import copy
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import release_rehearse_screening as screening
from release_cutover import CutoverError


SQL_ROOT = Path(__file__).resolve().parents[1] / "cloud/backend/db/migrations"


def archive(entries):
    """archive 在内存构造普通或链接tar条目，便于检验同名与恶意文件类型。"""
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w") as result:
        for name, content, kind in entries:
            item = tarfile.TarInfo(name)
            item.type = kind
            if kind == tarfile.REGTYPE:
                item.size = len(content)
                result.addfile(item, io.BytesIO(content))
            else:
                item.linkname = "../../outside.sql"
                result.addfile(item)
    return stream.getvalue()


def old_table(comment="旧业务表"):
    """old_table 返回与结构盘点一致的独立旧表元数据，保留所有者、约束及各类策略字段。"""
    return {"kind": "r", "persistence": "p", "owner": "postgres", "comment": comment,
            "partition_key": None, "row_security": False, "force_row_security": False,
            "options": None, "tablespace": 0, "replica_identity": "d",
            "columns": [{"name": "id", "type": "uuid", "not_null": True, "identity": "", "generated": "", "default": None, "comment": "旧编号"}],
            "indexes": {}, "constraints": {}, "triggers": {}, "policies": {}}


def source_tables():
    """source_tables 从实际五份SQL读取列、注释和显式索引，独立构造新表样例而不读取脚本白名单自证。"""
    result = {}
    for filename in screening.MIGRATIONS:
        sql = (SQL_ROOT / filename).read_text(encoding="utf-8")
        for match in re.finditer(r"CREATE TABLE IF NOT EXISTS (\w+)\s*\((.*?)\n\);", sql, re.S | re.I):
            name, body = match.groups()
            table = old_table()
            table_comment = re.search(r"COMMENT ON TABLE " + name + r" IS '([^']*)';", sql)
            table["comment"] = table_comment.group(1)
            table["columns"] = []
            for line in body.splitlines():
                column = re.match(r"\s*(\w+)\s+(DOUBLE PRECISION|TIMESTAMPTZ|UUID|BIGINT|JSONB|TEXT|DATE|BOOLEAN)\b(.*)", line, re.I)
                if not column:
                    continue
                key, kind, attributes = column.groups()
                description = re.search(r"COMMENT ON COLUMN " + name + r"\." + key + r" IS '([^']*)';", sql)
                default = re.search(r"\bDEFAULT\s+(.+?)(?:\s+CHECK\b|,?\s*$)", attributes, re.I)
                table["columns"].append({"name": key, "type": {"timestamptz": "timestamp with time zone"}.get(kind.lower(), kind.lower()),
                    "not_null": "NOT NULL" in attributes.upper() or "PRIMARY KEY" in attributes.upper(),
                    "identity": "", "generated": "", "default": default.group(1).lower().rstrip(",") if default else None,
                    "comment": description.group(1)})
                if "PRIMARY KEY" in attributes.upper():
                    table["indexes"][name + "_pkey"] = "CREATE UNIQUE INDEX " + name + "_pkey"
                    table["constraints"][name + "_pkey"] = "PRIMARY KEY (" + key + ")"
                if "CHECK" in attributes.upper():
                    table["constraints"][name + "_" + key + "_check"] = attributes.split("CHECK", 1)[1].strip().rstrip(",")
                if "REFERENCES" in attributes.upper():
                    table["constraints"][name + "_" + key + "_fkey"] = "FOREIGN KEY (" + key + ") " + attributes.split("REFERENCES", 1)[1].strip().rstrip(",")
            for constraint, fields in re.findall(r"\b(PRIMARY KEY|UNIQUE|FOREIGN KEY)\s*\(([^)]+)\)", body, re.I):
                columns = [value.strip() for value in fields.split(",")]
                suffix = "pkey" if constraint.upper() == "PRIMARY KEY" else "_".join(columns) + ("_key" if constraint.upper() == "UNIQUE" else "_fkey")
                key = (name + "_" + suffix)[:63]
                table["constraints"][key] = constraint + " (" + ", ".join(columns) + ")"
                if constraint.upper() != "FOREIGN KEY":
                    table["indexes"][key] = "CREATE UNIQUE INDEX " + key
            for index, definition in re.findall(r"CREATE (?:UNIQUE )?INDEX IF NOT EXISTS (\w+) (ON " + name + r"\b[^;]+);", sql, re.I):
                table["indexes"][index] = "CREATE INDEX " + index + " " + definition
            result["public." + name] = table
    return result


class ScreeningReleaseSafetyTests(unittest.TestCase):
    """验证完整旧快照和精确DDL边界，不把只读启动包装为业务写入或自动迁移验收。"""

    def setUp(self):
        """setUp 构造包含旧账本、旧数据及实际SQL新表的独立前后快照。"""
        self.ledger_before = [{"filename": "0001_initial.sql", "applied_at": "2026-10-01T00:00:00Z"}, {"filename": "0098_old.sql", "applied_at": "2026-10-02T00:00:00Z"}]
        self.ledger_after = copy.deepcopy(self.ledger_before) + [{"filename": name, "applied_at": "2026-10-26T10:00:00Z"} for name in screening.MIGRATIONS]
        self.before = {"public\t" + name: {"rows": 2, "sha256": hashlib.sha256(name.encode()).hexdigest()} for name in ["schema_migrations", "candidate_profiles", "ai_balance_records", "payment_refunds", "messages", "system_configs"]}
        self.after = copy.deepcopy(self.before)
        self.after["public\tschema_migrations"] = {"rows": 7, "sha256": "c" * 64}
        for name in source_tables():
            self.after[name.replace(".", "\t", 1)] = {"rows": 0, "sha256": screening.EMPTY_SHA256}
        self.schema_before = {name: {} for name in ["tables", "views", "functions", "aggregates", "sequences", "types", "namespaces", "extensions", "other_relations"]}
        for name in self.before:
            self.schema_before["tables"][name.replace("\t", ".", 1)] = old_table()
        self.schema_before["functions"]["public.existing_function()"] = {"definition": "unchanged", "owner": "postgres", "comment": "旧函数"}
        self.schema_after = copy.deepcopy(self.schema_before)
        self.schema_after["tables"].update(source_tables())
        triggers = self.schema_after["tables"]["public.candidate_profiles"]["triggers"]
        triggers["candidate_resume_ownership_version"] = {"enabled": "O", "definition": "CREATE TRIGGER candidate_resume_ownership_version BEFORE UPDATE OF tenant_id, created_by_user_id ON public.candidate_profiles FOR EACH ROW EXECUTE FUNCTION version_resume_ownership_change()"}
        triggers["candidate_resume_search_invalidation"] = {"enabled": "O", "definition": "CREATE TRIGGER candidate_resume_search_invalidation AFTER DELETE OR UPDATE ON public.candidate_profiles FOR EACH ROW EXECUTE FUNCTION enqueue_resume_search_invalidation()"}
        self.schema_after["tables"]["public.ai_balance_records"]["indexes"][screening.NEW_USAGE_INDEX] = "CREATE INDEX idx_ai_balance_records_usage_retention ON public.ai_balance_records USING btree (created_at, id) WHERE ((category = 'ai_usage'::text) AND (change_units < 0))"
        sql = (SQL_ROOT / "0102_resume_search_invalidation.sql").read_text(encoding="utf-8")
        for name, definition in re.findall(r"CREATE OR REPLACE FUNCTION (\w+)\(\)(.*?\$\$ LANGUAGE plpgsql;)", sql, re.S):
            self.schema_after["functions"]["public." + name + "()"] = {"definition": "CREATE OR REPLACE FUNCTION public." + name + "()" + definition, "owner": "postgres", "comment": "已审阅的旧副本清理函数"}

        self.expected_schema = copy.deepcopy(self.schema_after)

    def test_archive_keeps_sql_bytes_and_excludes_down_files(self):
        """只保留上行SQL原字节，down迁移及普通非SQL条目不进入执行集合。"""
        value = archive([("migrations/0001.sql", b"SELECT 1;\n", tarfile.REGTYPE), ("migrations/0001.down.sql", b"DROP TABLE users;", tarfile.REGTYPE), ("migrations/README.md", b"note", tarfile.REGTYPE)])
        self.assertEqual(screening.packaged_migrations(value), {"0001.sql": b"SELECT 1;\n"})

    def test_archive_rejects_repeated_basename_even_in_other_directories(self):
        """不同目录的同名迁移也必须拒绝，防止覆盖预期SQL。"""
        for other in ["migrations/0001.sql", "other/0001.sql"]:
            with self.subTest(other=other), self.assertRaises(CutoverError):
                screening.packaged_migrations(archive([("migrations/0001.sql", b"one", tarfile.REGTYPE), (other, b"two", tarfile.REGTYPE)]))

    def test_archive_rejects_links_and_nonregular_sql(self):
        """符号链接、硬链接、目录和设备条目不能作为可执行迁移。"""
        for kind in [tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.DIRTYPE, tarfile.CHRTYPE]:
            with self.subTest(kind=kind), self.assertRaises(CutoverError):
                screening.packaged_migrations(archive([("migrations/0001.sql", b"", kind)]))

    def test_archive_rejects_empty_oversized_or_excessive_migrations(self):
        """空归档、超过单文件限额、总字节限额和文件数量均不能进入迁移。"""
        cases = [archive([]), archive([("large.sql", b"x" * (4 * 1024**2 + 1), tarfile.REGTYPE)]), b"x" * (16 * 1024**2 + 1), archive([(f"{i:04}.sql", b"", tarfile.REGTYPE) for i in range(301)])]
        for value in cases:
            with self.subTest(length=len(value)), self.assertRaises(CutoverError):
                screening.packaged_migrations(value)

    def test_exact_ledger_upgrade_preserves_old_rows_and_times(self):
        """合法迁移仅追加五行，既有账本的所有字段和时间保持完全一致。"""
        screening.assert_ledger_upgrade(self.ledger_before, self.ledger_after)
        self.assertEqual(len(self.ledger_after) - len(self.ledger_before), 5)

    def test_ledger_rejects_old_changes_duplicates_and_missing_new_rows(self):
        """重写旧执行时间、旧字段、重复或缺少某一迁移均被拒绝。"""
        variants = []
        changed = copy.deepcopy(self.ledger_after); changed[0]["applied_at"] = "2026-10-26T12:00:00Z"; variants.append(changed)
        changed = copy.deepcopy(self.ledger_after); changed[0]["extra"] = "changed"; variants.append(changed)
        variants.extend([self.ledger_after + [self.ledger_after[-1]], self.ledger_after[:-1], self.ledger_after + [{"filename": "0104_unapproved.sql", "applied_at": "now"}]])
        changed = copy.deepcopy(self.ledger_after); changed[-1]["applied_at"] = ""; variants.append(changed)
        for after in variants:
            with self.subTest(after=after), self.assertRaises(CutoverError):
                screening.assert_ledger_upgrade(self.ledger_before, after)
        with self.assertRaises(CutoverError):
            screening.assert_ledger_upgrade(self.ledger_after, self.ledger_after)

    def test_business_upgrade_allows_only_seven_empty_new_tables(self):
        """七张新表为空且旧业务整行摘要不变时才能通过。"""
        screening.assert_business_preserved(self.before, self.after)
        self.assertEqual(len(self.after) - len(self.before), 7)

    def test_business_rejects_row_or_digest_changes_in_every_old_table(self):
        """简历、退款、消息和系统配置均与余额一样保护，不按业务类型放行。"""
        for name in self.before:
            if name == "public\tschema_migrations":
                continue
            for field, value in [("rows", 3), ("sha256", "0" * 64)]:
                changed = copy.deepcopy(self.after); changed[name][field] = value
                with self.subTest(name=name, field=field), self.assertRaises(CutoverError):
                    screening.assert_business_preserved(self.before, changed)

    def test_business_rejects_table_add_drop_or_preexisting_new_table(self):
        """不能漏旧表、加第八张表或把已存在的新功能表冒充本次新增。"""
        added = copy.deepcopy(self.after); added["public\tunapproved"] = {"rows": 0, "sha256": screening.EMPTY_SHA256}
        removed = copy.deepcopy(self.after); removed.pop("public\tmessages")
        for after in [added, removed]:
            with self.assertRaises(CutoverError):
                screening.assert_business_preserved(self.before, after)
        before = copy.deepcopy(self.before); before["public\tposition_screening_records"] = {"rows": 0, "sha256": screening.EMPTY_SHA256}
        with self.assertRaises(CutoverError):
            screening.assert_business_preserved(before, self.after)

    def test_new_tables_must_have_zero_rows_and_empty_digest(self):
        """每张新表都同时核对零行和空字节摘要，不接受伪零行数。"""
        for name in source_tables():
            for field, value in [("rows", 1), ("sha256", "a" * 64)]:
                changed = copy.deepcopy(self.after); changed[name.replace(".", "\t", 1)][field] = value
                with self.subTest(name=name, field=field), self.assertRaises(CutoverError):
                    screening.assert_business_preserved(self.before, changed)

    def test_actual_migrations_match_new_table_and_screening_column_contract(self):
        """直接核对实际SQL的七张表及0103十五列和七索引，避免白名单漏项。"""
        tables = source_tables(); self.assertEqual(set(tables), screening.NEW_TABLES)
        table = tables["public.position_screening_records"]
        expected_columns = {"id", "tenant_id", "position_id", "owner_user_id", "attempt_id", "revision", "occurred_at", "snapshot", "revision_hashes", "first_score", "second_score", "first_started_at", "second_started_at", "created_at", "updated_at"}
        self.assertEqual({column["name"] for column in table["columns"]}, expected_columns)
        self.assertEqual(len(table["indexes"]), 7)
        for name in ["first_score", "second_score", "first_started_at", "second_started_at"]:
            self.assertFalse(next(column for column in table["columns"] if column["name"] == name)["not_null"])
        self.assertTrue(all(column["comment"] for column in table["columns"]))

    def test_schema_accepts_only_documented_additions_without_mutating_inputs(self):
        """白名单内的表、函数、触发器和消费索引应通过，检查本身不能删改调用者快照。"""
        original = copy.deepcopy(self.schema_after)
        screening.assert_schema_upgrade(self.schema_before, self.schema_after, self.expected_schema)
        self.assertEqual(original, self.schema_after)

    def test_schema_rejects_any_old_table_ddl_change(self):
        """原字段、约束、旧索引、策略、注释和所有者不能被新增迁移顺带修改。"""
        for key, value in [("owner", "rehearsal_app"), ("comment", "改过"), ("columns", []), ("constraints", {"new_check": "CHECK(false)"}), ("indexes", {"extra_index": "changed"}), ("policies", {"new_policy": "changed"})]:
            changed = copy.deepcopy(self.schema_after); changed["tables"]["public.messages"][key] = value
            with self.subTest(key=key), self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)

    def test_schema_rejects_extra_or_missing_table_function_and_database_objects(self):
        """第八张表、额外函数或旧视图序列类型扩展变化都不能被新表例外掩盖。"""
        for area, key in [("tables", "public.extra"), ("functions", "public.extra()"), ("views", "public.extra"), ("sequences", "public.extra"), ("types", "public.extra"), ("extensions", "extra"), ("aggregates", "public.extra()"), ("other_relations", "public.extra")]:
            changed = copy.deepcopy(self.schema_after); changed[area][key] = {"definition": "unapproved"}
            with self.subTest(area=area), self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)
        changed = copy.deepcopy(self.schema_after); changed["functions"]["public.existing_function()"]["definition"] = "modified"
        with self.assertRaises(CutoverError):
            screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)

    def test_schema_rejects_unapproved_trigger_or_usage_index_changes(self):
        """除精确的两触发器和消费索引外，旧表新增对象不得通过。"""
        variants = []
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.candidate_profiles"]["triggers"]["extra"] = {"enabled": "O", "definition": "candidate_profiles"}; variants.append(changed)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.candidate_profiles"]["triggers"]["candidate_resume_ownership_version"]["enabled"] = "D"; variants.append(changed)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.ai_balance_records"]["indexes"].pop(screening.NEW_USAGE_INDEX); variants.append(changed)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.ai_balance_records"]["indexes"]["extra"] = "CREATE INDEX extra"; variants.append(changed)
        for after in variants:
            with self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, after, self.expected_schema)

    def test_schema_rejects_new_table_side_effects_and_missing_comments(self):
        """新表不能带触发器、策略、临时类型或没有说明的字段。"""
        for key, value in [("kind", "p"), ("persistence", "u"), ("comment", None), ("triggers", {"extra": {}}), ("policies", {"extra": {}})]:
            changed = copy.deepcopy(self.schema_after); changed["tables"]["public.position_screening_records"][key] = value
            with self.subTest(key=key), self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.position_screening_records"]["columns"][0]["comment"] = None
        with self.assertRaises(CutoverError):
            screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)

    def test_schema_rejects_extra_index_on_any_new_table(self):
        """七张新表的索引也必须有白名单，不能在新表上随意增加第八类对象。"""
        for name in source_tables():
            changed = copy.deepcopy(self.schema_after); changed["tables"][name]["indexes"]["unapproved_extra_index"] = "CREATE INDEX unapproved_extra_index ON " + name + " (id)"
            with self.subTest(name=name), self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)

    def test_schema_rejects_extra_column_on_new_screening_table(self):
        """即使额外字段有中文注释，也不能扩大预审筛选记录表的字段范围。"""
        changed = copy.deepcopy(self.schema_after)
        changed["tables"]["public.position_screening_records"]["columns"].append({"name": "unapproved", "type": "text", "comment": "额外字段"})
        with self.assertRaises(CutoverError):
            screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)

    def test_same_name_function_trigger_and_usage_index_definition_drift_rejected(self):
        """只保留批准对象名不够，同名函数、触发器和消费索引的实际定义必须与独立参考库一致。"""
        variants = []
        changed = copy.deepcopy(self.schema_after); changed["functions"]["public.version_resume_ownership_change()"]["definition"] += " SECURITY DEFINER"; variants.append(changed)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.candidate_profiles"]["triggers"]["candidate_resume_search_invalidation"]["definition"] = "CREATE TRIGGER candidate_resume_search_invalidation AFTER DELETE ON public.candidate_profiles EXECUTE FUNCTION unexpected()"; variants.append(changed)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.ai_balance_records"]["indexes"][screening.NEW_USAGE_INDEX] = "CREATE UNIQUE INDEX idx_ai_balance_records_usage_retention ON public.ai_balance_records(id)"; variants.append(changed)
        for after in variants:
            with self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, after, self.expected_schema)

    def test_new_table_column_type_default_nullability_and_constraint_drift_rejected(self):
        """新表的类型、默认值、可空性和约束也必须精确保持，不能只按列名放行。"""
        for key, value in [("type", "text"), ("default", "'forged'::text"), ("not_null", False)]:
            changed = copy.deepcopy(self.schema_after); changed["tables"]["public.position_screening_records"]["columns"][0][key] = value
            with self.subTest(key=key), self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)
        changed = copy.deepcopy(self.schema_after); changed["tables"]["public.position_screening_records"]["constraints"]["extra_constraint"] = "CHECK(false)"
        with self.assertRaises(CutoverError):
            screening.assert_schema_upgrade(self.schema_before, changed, self.expected_schema)

    def test_schema_requires_independent_reference(self):
        """缺少独立参考结构时禁止退回宽松的仅按名称检查。"""
        for expected in [None, {}, self.schema_before]:
            with self.assertRaises(CutoverError):
                screening.assert_schema_upgrade(self.schema_before, self.schema_after, expected)

    def test_reference_schema_uses_schema_only_clone_and_exact_input_statements(self):
        """参考库来自本轮恢复PG的纯结构拷贝，执行传入固定事务，期间不复制任何业务行。"""
        instance = screening.ScreeningRehearsal.__new__(screening.ScreeningRehearsal)
        instance.pg = "isolated-rehearsal-pg"; instance.args = SimpleNamespace(database="synthetic")
        instance.before_schema = self.schema_before; instance.psql = Mock()
        statements = ["BEGIN;", "SELECT 'approved';", "COMMIT;"]
        with patch.object(screening, "command", side_effect=[b"-- schema only", b"", json.dumps(self.schema_before).encode(), b"", json.dumps(self.expected_schema).encode()]) as command:
            self.assertEqual(instance.reference_schema(statements), self.expected_schema)
        self.assertEqual(command.call_count, 5)
        dump = command.call_args_list[0].args[0]
        self.assertIn("--schema-only", dump); self.assertIn("--no-owner", dump); self.assertIn("--no-acl", dump)
        self.assertIn("isolated-rehearsal-pg", dump)
        self.assertEqual(command.call_args_list[3].args[1], "\n".join(statements).encode())
        instance.psql.assert_called_once_with("CREATE DATABASE screening_schema_reference OWNER postgres;")

    def test_reference_schema_mismatch_stops_before_migrations(self):
        """空结构参考库未复现旧DDL时，必须在执行五份迁移前停止。"""
        instance = screening.ScreeningRehearsal.__new__(screening.ScreeningRehearsal)
        instance.pg = "isolated-rehearsal-pg"; instance.args = SimpleNamespace(database="synthetic")
        instance.before_schema = self.schema_before; instance.psql = Mock()
        changed = copy.deepcopy(self.schema_before); changed["tables"].pop("public.messages")
        with patch.object(screening, "command", side_effect=[b"-- schema only", b"", json.dumps(changed).encode()]) as command:
            with self.assertRaises(CutoverError):
                instance.reference_schema(["BEGIN;", "SELECT 'never execute';", "COMMIT;"])
        self.assertEqual(command.call_count, 3)

    def pair_runner(self, label, failure=""):
        """pair_runner 用内存桩驱动真实pair方法，记录权限窗口、启动、探测和失败收尾的实际顺序。"""
        instance = screening.ScreeningRehearsal.__new__(screening.ScreeningRehearsal)
        instance.prefix = "synthetic-rehearsal"; instance.args = SimpleNamespace(database="synthetic_test")
        instance.app_password = "private-synthetic"; instance.engine = Mock(); instance.engine.prefix = "/v1.47"
        instance.directory = Path("/synthetic/rehearsal"); instance.host = "synthetic-host"
        instance.before_filenames = [row["filename"] for row in self.ledger_before]
        instance.filenames = instance.before_filenames + list(screening.MIGRATIONS)
        instance.baseline = self.after; instance.migrated_ledger = self.ledger_after; instance.migrated_schema = self.schema_after
        instance.fingerprint = "e" * 64; instance.created = {}; instance.reports = {}
        instance.approved = {name: hashlib.sha256((SQL_ROOT / name).read_bytes()).hexdigest() for name in screening.MIGRATIONS}
        events, sqls = [], []
        images = {"backend": "sha256:" + "a" * 64, "frontend": "sha256:" + "b" * 64}

        def own(name, payload):
            """own 仅登记模拟本轮对象，不创建任何真实容器。"""
            instance.created[name] = name + "-id"
            return name + "-id"

        def start(identifier):
            """start 模拟指定服务启动失败，保留事件以核对grant发生在启动前。"""
            role = "backend" if "backend" in identifier else "frontend"; events.append("start:" + role)
            if failure == "start:" + role:
                raise CutoverError("模拟启动失败")

        def stop(identifier):
            """stop 模拟单个服务收尾失败，其他收尾和权限撤销仍应继续尝试。"""
            role = "backend" if "backend" in identifier else "frontend"; events.append("stop:" + role)
            if failure == "stop:" + role:
                raise CutoverError("模拟停止失败")

        def psql(sql):
            """psql 仅在内存区分grant、revoke与权限检查，不连接数据库。"""
            sqls.append(sql)
            event = "grant" if sql.startswith("GRANT") else "revoke" if sql.startswith("REVOKE") else "permissions"
            events.append(event)
            return b"t\n" if event == "permissions" and failure == "permissions" else b"f\n"

        def probe(frontend, url, expected, wait=False):
            """probe 模拟健康与后续业务路由，健康前后错误都不签发证明。"""
            event = "health" if url.endswith("/health") else "business"; events.append(event)
            if failure == event:
                raise CutoverError("模拟探测失败")
            return expected

        instance.own = own; instance.engine.start.side_effect = start; instance.stop = stop; instance.psql = psql; instance.probe = probe
        instance.database_snapshot = Mock(return_value=copy.deepcopy(self.after)); instance.ledger_records = Mock(return_value=self.ledger_after); instance.database_schema = Mock(return_value=self.schema_after)
        names = instance.filenames if label == "candidate" else instance.before_filenames
        packaged = archive([(name, (SQL_ROOT / name).read_bytes() if name in screening.MIGRATIONS else b"-- old", tarfile.REGTYPE) for name in names])
        return instance, images, events, sqls, packaged

    def test_pair_create_window_is_revoked_before_business_probes_for_both_pairs(self):
        """候选和完整回退都只在health前授CREATE，不给账本或业务写入，health后先撤销再探测业务。"""
        for label in ["candidate", "rollback"]:
            with self.subTest(label=label):
                instance, images, events, sqls, packaged = self.pair_runner(label)
                with patch.object(screening, "command", return_value=packaged), patch.object(screening, "save_json") as save:
                    instance.pair(label, images)
                self.assertLess(events.index("grant"), events.index("start:backend")); self.assertLess(events.index("health"), events.index("revoke"))
                self.assertLess(events.index("revoke"), events.index("permissions")); self.assertLess(events.index("permissions"), events.index("business"))
                self.assertEqual(events.count("revoke"), 2)
                self.assertEqual([sql for sql in sqls if sql.startswith("GRANT")], ["GRANT CREATE ON SCHEMA public TO rehearsal_app;"])
                self.assertTrue(save.call_args.args[1]["startup_create_permission_window"])
                self.assertFalse(save.call_args.args[1]["post_startup_write_and_create_permissions"])

    def test_pair_every_failure_attempts_both_stops_and_create_revoke_without_evidence(self):
        """任一启动、探测、权限或停止失败都必须尝试两端停止并撤回CREATE，不能留下成功报告。"""
        for failure in ["start:backend", "start:frontend", "health", "business", "permissions", "stop:backend", "stop:frontend"]:
            with self.subTest(failure=failure):
                instance, images, events, _, packaged = self.pair_runner("candidate", failure)
                with patch.object(screening, "command", return_value=packaged), patch.object(screening, "save_json") as save:
                    with self.assertRaises(CutoverError):
                        instance.pair("candidate", images)
                self.assertIn("stop:backend", events); self.assertIn("stop:frontend", events)
                self.assertEqual(events[-1], "revoke")
                self.assertEqual(instance.reports, {}); save.assert_not_called()

    def test_pair_keeps_post_startup_ledger_and_schema_gates(self):
        """即使权限窗口正常结束，数据摘要、迁移账本或结构漂移仍不得签发组合证据。"""
        for kind in ["data", "ledger", "schema"]:
            with self.subTest(kind=kind):
                instance, images, _, _, packaged = self.pair_runner("candidate")
                if kind == "data":
                    changed = copy.deepcopy(self.after); changed["public\tmessages"]["rows"] += 1; instance.database_snapshot.return_value = changed
                elif kind == "ledger":
                    instance.ledger_records.return_value = self.ledger_before
                else:
                    instance.database_schema.return_value = self.schema_before
                with patch.object(screening, "command", return_value=packaged), patch.object(screening, "save_json") as save:
                    with self.assertRaises(CutoverError):
                        instance.pair("candidate", images)
                save.assert_not_called()


if __name__ == "__main__":
    unittest.main()
