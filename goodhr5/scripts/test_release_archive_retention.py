"""文件作用：验证过期发布归档只删除精确旧文件，保护两套版本、分发包、失败恢复库和变化现场。"""

from contextlib import nullcontext
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

import release_archive_retention as archives


class ArchiveRetentionTests(unittest.TestCase):
    """使用临时目录验证归档边界，不连接Docker和生产数据。"""

    def test_candidate_boundaries(self):
        """保留当前、回退、较新归档、下载包、符号链接和失败恢复库，只选旧普通备份。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            paths = ["old-20261001/database.dump", "live-20261101/database.dump", "rollback-20261101/source.tar",
                     "old-20261001/source/public/downloads/package.tar", "old-20261001/report.json",
                     "failed-20261001/rehearsal/pgdata/PG_VERSION", "failed-20261001/database.dump",
                     "old-20261001/new.tar", "plugin-1.0-20261001/packages.tar"]
            for name in paths:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(name)
                os.utime(path, (1, 1))
            os.utime(root / "old-20261001/new.tar", None)
            (root / "old-20261001/link.dump").symlink_to(root / "old-20261001/database.dump")
            rows = archives.archive_candidates(root, {"live-20261101", "rollback-20261101"}, time.time() - 60)
            self.assertEqual([x["path"] for x in rows], [str(root / "old-20261001/database.dump")])

    def test_apply_rejects_changed_plan_without_deletion(self):
        """线上快照或待删清单有变化时，第一项也不得删除。"""
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary).resolve() / "database.dump"
            path.write_bytes(b"backup")
            plan = {"files": [{"path": str(path), "identity": archives.identity(path), "sha256": "invalid"}]}
            with patch.object(archives, "deployment_locks", return_value=nullcontext()), \
                    patch.object(archives, "make_plan", return_value={"changed": True}):
                with self.assertRaises(ValueError):
                    archives.execute_plan(plan, Path("registry"))
            self.assertTrue(path.exists())

    def test_apply_removes_only_verified_file(self):
        """精确计划通过后只移除该普通文件，其余证据和目录保留。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            path, proof = root / "database.dump", root / "report.json"
            path.write_bytes(b"backup")
            proof.write_text("keep")
            before = archives.identity(path)
            plan = {"files": [{"path": str(path), "identity": before, "sha256": archives.regular_file_sha(path, before)}]}
            with patch.object(archives, "deployment_locks", return_value=nullcontext()), \
                    patch.object(archives, "make_plan", return_value=plan):
                archives.execute_plan(plan, Path("registry"))
            self.assertFalse(path.exists())
            self.assertEqual(proof.read_text(), "keep")
