"""文件作用：验证未人工登记的新迁移实际经过副本执行、旧数据核对和绑定证据，零迁移不重复执行SQL。"""

import copy
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import Mock, patch

from release_cutover import CutoverError
from release_rehearse import Rehearsal, migration_fingerprint
from release_rehearse_auto import (AI_URL, AI_URL_MIGRATION, AI_URL_MIGRATION_SHA256,
                                   AutomaticUpgradeRehearsal, verify_ai_url_upgrade,
                                   verify_fact_retirement, without_retired_fact_data)


class AutomaticMigrationTests(unittest.TestCase):
    """用隔离的合成数据检查恢复流程，不连接真实数据库或Docker。"""

    def rehearsal(self, root, changed=False, applied=False):
        """构造有旧账号表和一份未登记新SQL的副本，记录执行语句及旧数据变化。"""
        source = root / "cloud/backend/db/migrations"
        source.mkdir(parents=True)
        old = {"0001_base.sql": b"CREATE TABLE users(email TEXT);"}
        new = {**old, "0125_identity.sql": b"CREATE TABLE identities(email TEXT REFERENCES users(email));"}
        for name, value in new.items():
            (source / name).write_bytes(value)
        job = object.__new__(AutomaticUpgradeRehearsal)
        job.args = SimpleNamespace(source_root=str(root))
        job.additive = None
        job.directory = root / "rehearsal"
        job.directory.mkdir()
        job.candidate, job.rollback = {"backend": "new"}, {"backend": "old"}
        job.filenames = list(new if applied else old)
        job.fingerprint = migration_fingerprint(job.filenames)
        schema = {"tables": {"public.users": {"columns": [{"name": "email"}]}}}
        updated_schema = copy.deepcopy(schema)
        updated_schema["tables"]["public.identities"] = {"columns": [{"name": "email"}]}
        before = [{"filename": name, "applied_at": "2020-01-01T00:00:00Z"} for name in job.filenames]
        after = before if applied else before + [{"filename": "0125_identity.sql", "applied_at": "2026-11-12T14:00:00.5Z"}]
        job.database_schema = Mock(side_effect=[updated_schema if applied else schema, updated_schema])
        job.ledger_records = Mock(side_effect=[before, after])
        baseline = {"public\tusers": {"rows": 1, "sha256": "unchanged-account"}}
        job.database_snapshot = Mock(side_effect=[baseline, {"public\tusers": {"rows": 0, "sha256": "changed"}} if changed else baseline, baseline])
        job.reference_schema = Mock(return_value=updated_schema)
        job.ledger = Mock(return_value=list(new))
        job.library_rows = Mock(side_effect=AssertionError("普通迁移不能使用默认话术的数据例外"))
        job.migration_archive = Mock(side_effect=[new, old])
        job.psql = Mock(side_effect=lambda sql: b"2026-11-12T14:00:00Z" if "clock_timestamp" in sql and job.psql.call_count == 1 else b"2026-11-12T14:00:01Z")
        return job

    def test_restore_omits_only_retired_statistics_data(self):
        """恢复清单只能省略统计明细数据项，业务数据、原表定义和索引都不得省略。"""
        toc = (b"; archive\n1; 1259 100 TABLE public system_stat_facts postgres\n"
               b"2; 0 101 TABLE DATA public users postgres\n"
               b"3; 0 100 TABLE DATA public system_stat_facts postgres\n"
               b"4; 1259 102 INDEX public system_stat_facts_pkey postgres\n")
        filtered = without_retired_fact_data(toc)
        self.assertIn(b"2; 0 101 TABLE DATA public users postgres\n", filtered)
        self.assertIn(b"1; 1259 100 TABLE public system_stat_facts postgres\n", filtered)
        self.assertIn(b"4; 1259 102 INDEX public system_stat_facts_pkey postgres\n", filtered)
        self.assertIn(b"\n; user-authorized retired statistics data: 3;", filtered)
        for invalid in (toc.replace(b"TABLE DATA public system_stat_facts", b"TABLE DATA public another_table"),
                        toc + b"5; 0 103 TABLE DATA public system_stat_facts postgres\n"):
            with self.assertRaises(CutoverError):
                without_retired_fact_data(invalid)

    def test_unregistered_migration_runs_and_records_frozen_evidence(self):
        """新SQL真正进入恢复流程，旧数据不变后才生成与动态摘要绑定的证据。"""
        with tempfile.TemporaryDirectory() as directory:
            job = self.rehearsal(Path(directory).resolve())
            with patch.object(Rehearsal, "restore"):
                job.restore()
            self.assertEqual(job.migrations, ("0125_identity.sql",))
            self.assertIn("CREATE TABLE identities", job.psql.call_args_list[1].args[0])
            self.assertIn("INSERT INTO public.schema_migrations", job.psql.call_args_list[1].args[0])
            self.assertTrue(all(call.kwargs["preserve_default_library"] is False for call in job.database_snapshot.call_args_list[:2]))
            evidence = json.loads((job.directory / "automatic-upgrade.json").read_text())
            self.assertEqual(set(evidence["approved_sha256"]), {"0125_identity.sql"})
            self.assertTrue(evidence["old_projected_data_preserved"])
            self.assertFalse(evidence["default_library_verified"])

    def test_old_data_change_cannot_produce_success_evidence(self):
        """副本中只要旧账号数据变化就停止，不签发成功记录，也不影响生产。"""
        with tempfile.TemporaryDirectory() as directory:
            job = self.rehearsal(Path(directory).resolve(), changed=True)
            with patch.object(Rehearsal, "restore"), self.assertRaises(CutoverError):
                job.restore()
            self.assertFalse((job.directory / "automatic-upgrade.json").exists())

    def test_already_applied_migration_is_not_run_again(self):
        """恢复库已带新账本而旧回退镜像缺少该文件时，只绑定摘要和回退检查，不重跑迁移。"""
        with tempfile.TemporaryDirectory() as directory:
            job = self.rehearsal(Path(directory).resolve(), applied=True)
            with patch.object(Rehearsal, "restore"):
                job.restore()
            self.assertEqual(job.migrations, ())
            self.assertEqual(job.before_filenames, ["0001_base.sql"])
            self.assertEqual(set(job.approved), {"0125_identity.sql"})
            job.reference_schema.assert_not_called()
            job.database_snapshot.assert_not_called()
            self.assertFalse(any("BEGIN;" in call.args[0] for call in job.psql.call_args_list))

    def test_retirement_requires_only_named_projection_and_backup(self):
        """只允许统计事实表变成无存储视图，拒绝缺备份、仍采集、改成物化视图或取消其他表。"""
        before = {"tables": {"public.system_stat_facts": {}, "public.users": {"triggers": {}}}}
        after = {"tables": {"public.users": {"triggers": {}}}, "views": {"public.system_stat_facts": {"kind": "v"}}, "functions": {}}
        snapshot = {"rows": 9000000, "sha256": "a" * 64}
        report = verify_fact_retirement(before, after, snapshot, "b" * 64, "c" * 64)
        self.assertEqual(report["before"], snapshot)
        self.assertEqual(report["replacement"], "non_materialized_view")
        invalid = copy.deepcopy(after)
        invalid["views"]["public.system_stat_facts"]["kind"] = "m"
        with self.assertRaises(CutoverError):
            verify_fact_retirement(before, invalid, snapshot, "b" * 64, "c" * 64)
        invalid = copy.deepcopy(after)
        invalid["tables"]["public.users"]["triggers"]["system_stat_login"] = {}
        with self.assertRaises(CutoverError):
            verify_fact_retirement(before, invalid, snapshot, "b" * 64, "c" * 64)
        with self.assertRaises(CutoverError):
            verify_fact_retirement(before, after, snapshot, "", "c" * 64)
        with self.assertRaises(CutoverError):
            verify_fact_retirement({"tables": {"public.users": {}}}, after, snapshot, "b" * 64, "c" * 64)

    def test_retirement_does_not_exempt_business_rows(self):
        """取消统计投影时仍比较全部账号数据，账号变化不能凭退役例外获得成功证据。"""
        for changed in (False, True):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                job = self.rehearsal(root, changed=changed)
                source = root / "cloud/backend/db/migrations"
                name = "0126_retire_system_stat_facts.sql"
                sql = b"DROP TABLE system_stat_facts; CREATE VIEW system_stat_facts AS SELECT email FROM users;"
                (source / name).write_bytes(sql)
                packaged = {path.name: path.read_bytes() for path in source.glob("*.sql")}
                job.migration_archive = Mock(side_effect=[packaged, {"0001_base.sql": packaged["0001_base.sql"]}])
                before_schema = {"tables": {"public.users": {"columns": [{"name": "email"}]},
                                            "public.system_stat_facts": {"columns": [{"name": "event_key"}]}},
                                 "views": {}, "functions": {"public.capture_system_stat()": {}}}
                after_schema = {"tables": {"public.users": {"columns": [{"name": "email"}]},
                                           "public.identities": {"columns": [{"name": "email"}]}},
                                "views": {"public.system_stat_facts": {"kind": "v"}}, "functions": {}}
                job.database_schema = Mock(side_effect=[before_schema, after_schema])
                job.reference_schema = Mock(return_value=after_schema)
                job.baseline = {"public\tusers": {"rows": 1, "sha256": "user"},
                                "public\tsystem_stat_facts": {"rows": 100, "sha256": "a" * 64}}
                (job.directory / "database.dump").write_bytes(b"complete-synthetic-backup")
                before = [{"filename": "0001_base.sql", "applied_at": "2020-01-01T00:00:00Z"}]
                after = before + [{"filename": filename, "applied_at": "2026-11-12T14:00:00.5Z"}
                                  for filename in ("0125_identity.sql", name)]
                job.ledger_records = Mock(side_effect=[before, after])
                job.ledger = Mock(return_value=list(packaged))
                with patch.object(Rehearsal, "restore"):
                    if changed:
                        with self.assertRaises(CutoverError):
                            job.restore()
                        self.assertFalse((job.directory / "automatic-upgrade.json").exists())
                    else:
                        job.restore()
                        evidence = json.loads((job.directory / "automatic-upgrade.json").read_text())
                        self.assertFalse(evidence["old_projected_data_preserved"])
                        self.assertTrue(evidence["all_business_data_preserved"])
                        self.assertEqual(evidence["retired_non_business_projections"][0]["table"], "public.system_stat_facts")
                self.assertEqual(set(job.database_snapshot.call_args_list[0].args[0]), {"public\tusers"})


class AIURLUpgradeTests(unittest.TestCase):
    """验证唯一批准的AI域名替换，其他完整行、JSON字段及表数据仍严格保护。"""

    def rows(self):
        """构造带自定义接口、密钥、模型和系统额外字段的合成完整行。"""
        old = "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions"
        return {"user_ai_configs": [
            {"id": "user-config", "user_id": "user", "base_url": old,
             "model": "keep-model", "api_key_encrypted": "synthetic-secret", "temperature": 0.35,
             "enabled": False, "updated_at": "2020-01-01T00:00:00Z"},
            {"id": "custom-config", "base_url": "https://custom.example/v1", "model": "custom-model"},
        ], "system_configs": [
            {"id": "app", "config_key": "system.app_config", "config_value": {
                "builtin_ai": {"public_base_url": old, "upstream_api_key": "synthetic-upstream",
                               "default_model": "keep-model", "models": [{"id": "keep-model"}]},
                "keep": {"value": 1}}, "updated_at": "2020-01-01T00:00:00Z"},
            {"id": "other", "config_key": "system.other", "config_value": {"keep": True}},
        ]}

    def upgraded_rows(self):
        """独立构造只更新两个获准URL字段的迁移结果。"""
        rows = self.rows()
        rows["user_ai_configs"][0]["base_url"] = AI_URL
        rows["system_configs"][0]["config_value"]["builtin_ai"]["public_base_url"] = AI_URL
        return rows

    def test_only_approved_urls_change_and_evidence_has_no_secrets(self):
        """合法改动保留密钥模型和时间，证据仅含数量、迁移身份与核对结论。"""
        report = verify_ai_url_upgrade(self.rows(), self.upgraded_rows())
        self.assertTrue(report["exact_url_data_change_verified"])
        self.assertEqual(report["changed_rows"], {"user_ai_configs": 1, "system_configs": 1})
        self.assertIn("except_verified_builtin_ai_public_urls", report["legacy_data_scope"])
        self.assertEqual(len(report["allowed_data_changes"]), 2)
        self.assertNotIn("synthetic-secret", json.dumps(report))
        sql = Path(__file__).resolve().parents[1] / "cloud/backend/db/migrations" / AI_URL_MIGRATION
        self.assertEqual(hashlib.sha256(sql.read_bytes()).hexdigest(), AI_URL_MIGRATION_SHA256)

    def test_other_fields_or_unapproved_addresses_cannot_change(self):
        """拒绝改密钥模型、漏字段、时间、第三方地址、其他系统JSON及新增删除配置行。"""
        changes = [
            (("user_ai_configs", 0, "api_key_encrypted"), "changed-secret"),
            (("user_ai_configs", 0, "model"), "changed-model"),
            (("user_ai_configs", 0, "temperature"), 0.9),
            (("user_ai_configs", 0, "enabled"), True),
            (("user_ai_configs", 0, "enabled"), 0),
            (("user_ai_configs", 0, "updated_at"), "2026-11-25T00:00:00Z"),
            (("user_ai_configs", 0, "base_url"), AI_URL + "?unapproved=1"),
            (("user_ai_configs", 1, "base_url"), AI_URL),
            (("system_configs", 0, "config_value", "keep", "value"), 2),
            (("system_configs", 0, "config_value", "builtin_ai", "upstream_api_key"), "changed"),
            (("system_configs", 0, "config_value", "builtin_ai", "default_model"), "changed"),
            (("system_configs", 0, "updated_at"), "2026-11-25T00:00:00Z"),
            (("system_configs", 1, "config_value", "keep"), False),
            (("system_configs", 1, "config_value", "keep"), 1),
        ]
        for path, value in changes:
            with self.subTest(path=path):
                after = self.upgraded_rows()
                target = after
                for key in path[:-1]:
                    target = target[key]
                target[path[-1]] = value
                with self.assertRaises(CutoverError):
                    verify_ai_url_upgrade(self.rows(), after)
        for table in ("user_ai_configs", "system_configs"):
            for operation in ("remove", "duplicate", "insert", "remove-field"):
                with self.subTest(table=table, operation=operation):
                    after = self.upgraded_rows()
                    if operation == "remove":
                        after[table].pop()
                    elif operation == "duplicate":
                        after[table].append(copy.deepcopy(after[table][0]))
                    elif operation == "insert":
                        after[table].append(dict(after[table][0], id="new-row"))
                    else:
                        after[table][0].pop("updated_at")
                    with self.assertRaises(CutoverError):
                        verify_ai_url_upgrade(self.rows(), after)

    def test_empty_and_legacy_system_values_follow_fixed_sql(self):
        """缺失或null内置配置可以补URL，无效JSON结构和非空外部地址仍保留。"""
        for before_value, expected in (
                ({}, {"builtin_ai": {"public_base_url": AI_URL}}),
                ({"builtin_ai": None}, {"builtin_ai": {"public_base_url": AI_URL}}),
                ({"builtin_ai": {"public_base_url": None, "keep": 1}}, {"builtin_ai": {"public_base_url": AI_URL, "keep": 1}}),
                ({"builtin_ai": {"public_base_url": "  "}}, {"builtin_ai": {"public_base_url": AI_URL}}),
                ({"builtin_ai": {"public_base_url": " HTTPS://GOODHR5.58IT.CN/api/ai-compatible/v1/chat/completions "}}, {"builtin_ai": {"public_base_url": AI_URL}}),
                ({"builtin_ai": []}, {"builtin_ai": []}),
                ({"builtin_ai": {"public_base_url": "\t"}}, {"builtin_ai": {"public_base_url": "\t"}}),
                ({"builtin_ai": {"public_base_url": 42}}, {"builtin_ai": {"public_base_url": 42}}),
                (None, None)):
            with self.subTest(before_value=before_value):
                before = self.rows()
                before["system_configs"][0]["config_value"] = before_value
                after = self.upgraded_rows()
                after["system_configs"][0]["config_value"] = expected
                verify_ai_url_upgrade(before, after)
        empty = {"user_ai_configs": [], "system_configs": []}
        self.assertEqual(verify_ai_url_upgrade(empty, empty)["changed_rows"], {"user_ai_configs": 0, "system_configs": 0})

    def test_personal_urls_match_only_fixed_complete_routes(self):
        """只接受三个旧完整HTTPS地址，空格大小写兼容且不误改相似域名、查询参数和其他路由。"""
        for value, changes in (
                ("  HTTPS://GOODHR.AI.58IT.CN/api/ai-compatible/v1/chat/completions ", True),
                ("https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions", True),
                ("https://goodhr.58it.cn.evil.invalid/api/ai-compatible/v1/chat/completions", False),
                ("https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions?x=1", False),
                ("https://goodhr.58it.cn/other-route", False),
                ("\thttps://goodhr.58it.cn/api/ai-compatible/v1/chat/completions\t", False),
                ("", False), (AI_URL, False)):
            with self.subTest(value=value):
                before = self.rows()
                before["user_ai_configs"][0]["base_url"] = value
                after = self.upgraded_rows()
                after["user_ai_configs"][0]["base_url"] = AI_URL if changes else value
                verify_ai_url_upgrade(before, after)

    def test_restore_uses_exact_rows_only_for_frozen_migration(self):
        """固定SQL才启用两表逐行校验，其他旧表仍走摘要，变更SQL或配置内容则停止。"""
        sql = (Path(__file__).resolve().parents[1] / "cloud/backend/db/migrations" / AI_URL_MIGRATION).read_bytes()
        for failure in (None, "sql", "config"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                job = AutomaticMigrationTests().rehearsal(root)
                source = root / "cloud/backend/db/migrations"
                (source / AI_URL_MIGRATION).write_bytes(sql + (b"\n" if failure == "sql" else b""))
                packaged = {path.name: path.read_bytes() for path in source.glob("*.sql")}
                job.migration_archive = Mock(side_effect=[packaged, {"0001_base.sql": packaged["0001_base.sql"]}])
                before_schema = {"tables": {"public.users": {"columns": [{"name": "email"}]},
                    "public.user_ai_configs": {"columns": [{"name": "id"}, {"name": "base_url"}]},
                    "public.system_configs": {"columns": [{"name": "id"}, {"name": "config_value"}]}}}
                after_schema = copy.deepcopy(before_schema)
                after_schema["tables"]["public.identities"] = {"columns": [{"name": "email"}]}
                job.database_schema = Mock(side_effect=[before_schema, after_schema])
                job.reference_schema = Mock(return_value=after_schema)
                before_ledger = [{"filename": "0001_base.sql", "applied_at": "2020-01-01T00:00:00Z"}]
                after_ledger = before_ledger + [{"filename": name, "applied_at": "2026-11-12T14:00:00.5Z"}
                    for name in ("0125_identity.sql", AI_URL_MIGRATION)]
                job.ledger_records = Mock(side_effect=[before_ledger, after_ledger])
                job.ledger = Mock(return_value=list(packaged))
                after_rows = self.upgraded_rows()
                if failure == "config":
                    after_rows["user_ai_configs"][0]["api_key_encrypted"] = "changed"
                job.ai_url_rows = Mock(side_effect=[self.rows(), after_rows])
                with patch.object(Rehearsal, "restore"):
                    if failure:
                        with self.assertRaises(CutoverError):
                            job.restore()
                        self.assertFalse((job.directory / "automatic-upgrade.json").exists())
                    else:
                        job.restore()
                        evidence = json.loads((job.directory / "automatic-upgrade.json").read_text())
                        self.assertTrue(evidence["builtin_ai_url_change"]["exact_url_data_change_verified"])
                        self.assertIn("except_verified_builtin_ai_public_urls", evidence["legacy_data_scope"])
                        self.assertEqual(len(evidence["allowed_data_changes"]), 2)
                        self.assertEqual(set(job.database_snapshot.call_args_list[0].args[0]), {"public\tusers"})
                        self.assertEqual(job.ai_url_rows.call_count, 2)


if __name__ == "__main__":
    unittest.main()
