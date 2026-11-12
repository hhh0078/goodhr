"""文件作用：验证未人工登记的新迁移实际经过副本执行、旧数据核对和绑定证据，零迁移不重复执行SQL。"""

import copy
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import Mock, patch

from release_cutover import CutoverError
from release_rehearse import Rehearsal, migration_fingerprint
from release_rehearse_auto import AutomaticUpgradeRehearsal, verify_fact_retirement


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


if __name__ == "__main__":
    unittest.main()
