"""文件作用：验证过期发布归档只删除精确旧文件，保护两套版本、分发包、失败恢复库和变化现场。"""

from contextlib import nullcontext
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

import release_archive_retention as archives


class ArchiveRetentionTests(unittest.TestCase):
    """使用临时目录验证归档边界，不连接Docker和生产数据。"""

    def image_archive(self, path):
        """创建包含可核对配置摘要的真实Docker导出包，避免用假文本代替归档验证。"""
        config = b'{"architecture":"amd64"}'
        digest = hashlib.sha256(config).hexdigest()
        manifest = json.dumps([{"Config": digest + ".json", "Layers": []}]).encode()
        with archives.tarfile.open(path, "w") as archive:
            for name, content in (("manifest.json", manifest), (digest + ".json", config)):
                member = archives.tarfile.TarInfo(name)
                member.size = len(content)
                archive.addfile(member, io.BytesIO(content))
        os.utime(path, (1, 1))
        return "sha256:" + digest

    def test_candidate_boundaries(self):
        """数据库、恢复包、源码及未知归档始终保留，只选择不再使用的真实镜像导出包。"""
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
            image = root / "old-20261001/images.tar"
            protected_id = self.image_archive(image)
            self.image_archive(root / "live-20261101/images.tar")
            self.image_archive(root / "rollback-20261101/images.tar")
            (root / "old-20261001/link.tar").symlink_to(image)
            for name in ("database.dump.gz", "backup.sql.gz", "database.backup", "pgdata.tar.gz", "unknown.tgz"):
                path = root / "old-20261001" / name
                path.write_bytes(b"database must stay")
                os.utime(path, (1, 1))
            keep = {"live-20261101", "rollback-20261101"}
            rows = archives.archive_candidates(root, keep, time.time() - 60)
            self.assertEqual([x["path"] for x in rows], [str(image)])
            self.assertEqual(archives.archive_candidates(root, keep, time.time() - 60, {protected_id}), [])

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

    def test_docker_archive_mixed_with_database_is_preserved(self):
        """归档即使带合法镜像manifest，混有数据库文件时也不得进入清理清单。"""
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "mixed.tar"
            self.image_archive(path)
            with archives.tarfile.open(path, "a") as archive:
                member = archives.tarfile.TarInfo("database.dump")
                member.size = 4
                archive.addfile(member, io.BytesIO(b"data"))
            self.assertEqual(archives.image_archive_ids(path), set())

    def test_apply_removes_only_verified_file(self):
        """精确计划通过后只移除该普通文件，其余证据和目录保留。"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            path, proof = root / "images.tar", root / "database.dump"
            self.image_archive(path)
            proof.write_text("keep")
            before = archives.identity(path)
            plan = {"files": [{"path": str(path), "identity": before, "sha256": archives.regular_file_sha(path, before)}]}
            with patch.object(archives, "deployment_locks", return_value=nullcontext()), \
                    patch.object(archives, "make_plan", return_value=plan):
                archives.execute_plan(plan, Path("registry"))
            self.assertFalse(path.exists())
            self.assertEqual(proof.read_text(), "keep")
