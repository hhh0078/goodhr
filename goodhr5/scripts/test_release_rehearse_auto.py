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
from release_rehearse_auto import AutomaticUpgradeRehearsal


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


if __name__ == "__main__":
    unittest.main()
