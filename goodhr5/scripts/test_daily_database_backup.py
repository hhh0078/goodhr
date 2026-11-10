"""文件作用：在独立临时目录验证每日备份的七份轮换、失败保护、损坏检测与数据库归属，不调用真实Docker或生产数据库。"""

import hashlib
import importlib.util
import json
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("daily_database_backup", Path(__file__).with_name("daily-database-backup.py"))
backup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(backup)
TOC = b"1; 1259 1 TABLE public synthetic postgres\n"


class DailyBackupTests(unittest.TestCase):
    """使用合成PGDMP归档覆盖成功与失败，真实目录只由TemporaryDirectory创建和回收。"""

    def setUp(self):
        """setUp 替换固定备份根及外部命令，禁止测试触达服务器或现有本机数据库。"""
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name).resolve() / "daily"
        self.root.mkdir()
        self.patches = [patch.object(backup, "ROOT", self.root),
                        patch.object(backup, "production_database", return_value=("goodhr_test", "p" * 64)),
                        patch.object(backup.shutil, "disk_usage", return_value=type("Space", (), {"free": 20 * 1024**3})()),
                        patch.object(backup, "command", side_effect=self.command)]
        for item in self.patches:
            item.start()
        self.fail_dump = False

    def tearDown(self):
        """tearDown 先恢复命令和路径，再删除本测试创建的临时目录。"""
        for item in reversed(self.patches):
            item.stop()
        self.temporary.cleanup()

    def command(self, arguments, **kwargs):
        """command 只模拟固定归档生成及TOC读取，其他命令立即让测试失败。"""
        if "--list" in arguments:
            return TOC
        if any("exec pg_dump" in value for value in arguments):
            if self.fail_dump:
                kwargs["output"].write(b"PGDMP incomplete")
                raise backup.CutoverError("模拟备份中途失败")
            kwargs["output"].write(b"PGDMP synthetic successful archive")
            return None
        raise AssertionError(arguments)

    def seed(self, day, database="goodhr_test", year=2026):
        """seed 创建同任务同主机的合成历史备份及真实内容摘要，日期仅决定明确保留次序。"""
        name = "goodhr-%04d01%02dT060000000000Z-%08x.dump" % (year, day, day)
        archive = self.root / name
        archive.write_bytes(b"PGDMP synthetic " + str(day).encode())
        value = {"schema_version": 1, "owner": backup.OWNER, "host": socket.gethostname(), "database": database,
                 "file": name, "size_bytes": archive.stat().st_size, "sha256": backup.file_sha(archive),
                 "toc_sha256": hashlib.sha256(TOC).hexdigest(), "verified": True}
        backup.save_manifest(self.root / (name + ".manifest.json"), value)
        return archive

    def test_verified_new_backup_keeps_exactly_seven_and_preserves_unrelated_files(self):
        """新归档完成后只轮换本任务超出七份的旧备份，其他数据库、发布材料和未知文件保持原样。"""
        originals = [self.seed(day) for day in range(1, 9)]
        foreign = self.seed(10, "unrelated_test")
        original = self.root / "database.dump"
        original.write_bytes(b"protected release backup")
        result = backup.backup_once()
        self.assertTrue(result["success"])
        self.assertEqual(result["kept"], 7)
        self.assertEqual(result["removed"], [p.name for p in originals[:2]])
        self.assertEqual(len(backup.existing_backups("goodhr_test", "p" * 64)), 7)
        self.assertTrue(foreign.exists())
        self.assertEqual(original.read_bytes(), b"protected release backup")
        self.assertFalse(list(self.root.glob("*.partial")))

    def test_dump_failure_keeps_every_old_backup_and_removes_only_own_partial(self):
        """生成失败不先删除任何旧备份，也不会把本轮半成品标记为成功。"""
        originals = [self.seed(day) for day in range(1, 9)]
        self.fail_dump = True
        with self.assertRaises(backup.CutoverError):
            backup.backup_once()
        self.assertTrue(all(path.exists() for path in originals))
        self.assertFalse(list(self.root.glob("*.partial")))
        self.assertEqual(len(list(self.root.glob("*.dump"))), 8)

    def test_corruption_and_low_space_never_delete_valid_old_backups(self):
        """损坏或空间不足都停止轮换，保留每一份原有备份。"""
        archive = self.seed(1)
        archive.write_bytes(b"corrupted")
        with self.assertRaises(backup.CutoverError):
            backup.backup_once()
        self.assertTrue(archive.exists())
        archive.unlink()
        (self.root / (archive.name + ".manifest.json")).unlink()
        valid = self.seed(2)
        with patch.object(backup.shutil, "disk_usage", return_value=type("Space", (), {"free": 100})()):
            with self.assertRaises(backup.CutoverError):
                backup.backup_once()
        self.assertTrue(valid.exists())

    def test_linked_archive_is_never_read_or_deleted(self):
        """备份文件被链接替换后停止清理，绝不触碰链接指向的外部文件。"""
        archive = self.seed(1)
        outside = self.root.parent / "protected.dump"
        outside.write_bytes(archive.read_bytes())
        archive.unlink()
        archive.symlink_to(outside)
        with self.assertRaises(backup.CutoverError):
            backup.backup_once()
        self.assertTrue(outside.exists())

    def test_clock_regression_keeps_new_and_all_existing_archives(self):
        """系统时钟倒退时不把刚完成的新备份当成最旧记录删除，保留全部归档等待核对。"""
        originals = [self.seed(day, year=2099) for day in range(1, 8)]
        with self.assertRaises(backup.CutoverError):
            backup.backup_once()
        self.assertTrue(all(path.exists() for path in originals))
        self.assertEqual(len(list(self.root.glob("*.dump"))), 8)

    def test_database_requires_actual_host_port_mapping(self):
        """同名数据库不足以证明归属；DSN必须指向已运行共享PG容器发布的本机端口。"""
        self.patches[1].stop()
        rows = [{"State": {"Running": True}, "Config": {"Env": ["GOODHR_PG_DSN=postgres://example:secret@127.0.0.1:5432/goodhr_test"]}, "HostConfig": {"NetworkMode": "host"}},
                {"Id": "p" * 64, "State": {"Running": True}, "NetworkSettings": {"Ports": {"5432/tcp": [{"HostIp": "0.0.0.0", "HostPort": "5432"}]}}}]
        with patch.object(backup, "command", return_value=json.dumps(rows).encode()):
            self.assertEqual(backup.production_database(), ("goodhr_test", "p" * 64))
            rows[0]["Config"]["Env"] = ["GOODHR_PG_DSN=postgres://example:secret@other-host/goodhr_test"]
        with patch.object(backup, "command", return_value=json.dumps(rows).encode()):
            with self.assertRaises(backup.CutoverError):
                backup.production_database()


if __name__ == "__main__":
    unittest.main()
