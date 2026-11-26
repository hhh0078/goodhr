"""文件作用：用临时真实 SQLite 验证在线 WAL 备份、幂等校验、路径边界和失败现场，不接触生产数据。"""

import datetime
import contextlib
import fcntl
import json
import os
from pathlib import Path
import sqlite3
import stat
import tempfile
import unittest
from unittest import mock

import panel_database_backup as backup


class PanelDatabaseBackupTests(unittest.TestCase):
    """每个测试使用隔离来源、目标和既有维护锁。"""

    def setUp(self):
        """创建临时路径并替换固定生产常量，不安装任何依赖。"""
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.source = self.root / 'source'
        self.source.mkdir()
        self.destination = self.root / 'backups'
        self.lock = self.root / 'maintenance.lock'
        self.lock.touch(mode=0o600)
        for name, value in [('SOURCE_ROOT', self.source), ('BACKUP_ROOT', self.destination), ('LOCK_PATH', self.lock)]:
            patcher = mock.patch.object(backup, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        self.day = datetime.date.today().isoformat()

    def database(self, name, wal=False):
        """建立带已提交数据的真实数据库，WAL 场景保留连接以确保日志尚未合并。"""
        path = self.source / name
        path.parent.mkdir(parents=True, exist_ok=True)
        connection = sqlite3.connect(str(path))
        if wal:
            connection.execute('PRAGMA journal_mode=WAL')
            connection.execute('PRAGMA wal_autocheckpoint=0')
        connection.execute('CREATE TABLE sample(value TEXT)')
        connection.execute("INSERT INTO sample VALUES ('committed-in-wal')")
        connection.commit()
        self.addCleanup(connection.close)
        return path, connection

    def test_preview_and_real_wal_backup_are_private_and_source_unchanged(self):
        """预览不写目标；在线副本包含未合并 WAL，保留源文件、相对目录与私有权限。"""
        source, connection = self.database('db/site.db', wal=True)
        self.database('nested/second/state.sqlite3')
        self.database('agent/ignored.db')
        self.database('hids_data/ignored.sqlite')
        wal = Path(str(source) + '-wal')
        self.assertTrue(wal.exists())
        before = {path: path.read_bytes() for path in (source, wal)}
        self.assertEqual(backup.backup()['database_count'], 2)
        self.assertFalse(self.destination.exists())
        result = backup.backup(True)
        self.assertEqual(result['database_count'], 2)
        target = self.destination / self.day
        with contextlib.closing(sqlite3.connect(str(target / 'db/site.db'))) as copied, copied:
            self.assertEqual(copied.execute('SELECT value FROM sample').fetchall(), [('committed-in-wal',)])
        self.assertEqual({path: path.read_bytes() for path in (source, wal)}, before)
        self.assertEqual(connection.execute('SELECT COUNT(*) FROM sample').fetchone(), (1,))
        for current, directories, files in os.walk(str(target)):
            self.assertEqual(stat.S_IMODE(Path(current).stat().st_mode), 0o700)
            for name in files:
                self.assertEqual(stat.S_IMODE((Path(current) / name).stat().st_mode), 0o600)

    def test_valid_snapshot_is_idempotent_and_hash_tampering_is_refused(self):
        """当日有效清单幂等跳过，即使副本仍是合法 SQLite，摘要变动也必须拒绝。"""
        self.database('default.db')
        backup.backup(True)
        target = self.destination / self.day
        manifest = target / 'verified.json'
        before = manifest.read_bytes()
        with mock.patch.object(backup, 'copy_database', side_effect=AssertionError('不应重复备份')):
            self.assertTrue(backup.backup(True)['skipped_verified'])
        self.assertEqual(manifest.read_bytes(), before)
        with contextlib.closing(sqlite3.connect(str(target / 'default.db'))) as changed, changed:
            changed.execute("INSERT INTO sample VALUES ('tampered')")
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        self.assertEqual(manifest.read_bytes(), before)

    def test_source_links_and_overlapping_roots_are_refused(self):
        """来源数据库及目录链接、重叠根目录均拒绝，不创建任何目标备份。"""
        path, connection = self.database('default.db')
        outside = self.root / 'outside.db'
        outside.write_bytes(path.read_bytes())
        path.unlink()
        path.symlink_to(outside)
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        path.unlink()
        path.write_bytes(outside.read_bytes())
        (self.source / 'linked-dir').symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        (self.source / 'linked-dir').unlink()
        with mock.patch.object(backup, 'BACKUP_ROOT', self.source / 'nested'):
            with self.assertRaises(RuntimeError):
                backup.backup(True)
        self.assertFalse(self.destination.exists())

    def test_destination_links_and_manifest_traversal_are_refused(self):
        """目标根目录、当日文件链接及清单路径穿越都停止，不能读取或覆盖外部文件。"""
        self.database('default.db')
        outside = self.root / 'outside'
        outside.mkdir(mode=0o700)
        self.destination.symlink_to(outside, target_is_directory=True)
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        self.destination.unlink()
        backup.backup(True)
        target = self.destination / self.day
        path = target / 'default.db'
        saved = outside / 'saved.db'
        path.rename(saved)
        path.symlink_to(saved)
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        path.unlink()
        saved.rename(path)
        manifest = target / 'verified.json'
        value = json.loads(manifest.read_text())
        value['databases'][0]['path'] = '../outside.db'
        manifest.write_text(json.dumps(value))
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        self.assertEqual(list(outside.iterdir()), [])

    def test_corruption_or_timeout_keeps_failure_directory_without_finished_snapshot(self):
        """损坏库或超时留下私有失败目录，不能生成当天成品，也不删除旧数据库备份。"""
        self.database('a-valid.db')
        (self.source / 'z-broken.db').write_bytes(b'not-a-database')
        self.destination.mkdir(mode=0o700)
        old = self.destination / 'old-database.dump'
        old.write_bytes(b'protected-old-backup')
        with self.assertRaises(sqlite3.DatabaseError):
            backup.backup(True)
        self.assertFalse((self.destination / self.day).exists())
        failures = list(self.destination.glob('.incomplete-*'))
        self.assertEqual(len(failures), 1)
        self.assertTrue((failures[0] / 'failed.json').is_file())
        self.assertFalse((failures[0] / 'verified.json').exists())
        self.assertEqual(old.read_bytes(), b'protected-old-backup')
        with self.assertRaises(RuntimeError):
            backup.backup(True)
        self.assertEqual(len(list(self.destination.glob('.incomplete-*'))), 1)
        (self.source / 'z-broken.db').unlink()
        timeout_root = self.root / 'timeout-backups'
        with mock.patch.object(backup, 'BACKUP_ROOT', timeout_root), mock.patch.object(backup, 'DATABASE_TIMEOUT', -1):
            with self.assertRaises(TimeoutError):
                backup.backup(True)
        self.assertFalse((timeout_root / self.day).exists())
        self.assertEqual(len(list(timeout_root.glob('.incomplete-*'))), 1)

    def test_existing_maintenance_lock_prevents_concurrent_backup(self):
        """已有维护锁被占用时立即退出，不创建目标或启动第二轮数据库备份。"""
        self.database('default.db')
        with self.lock.open('r+') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaises(BlockingIOError):
                backup.backup(True)
        self.assertFalse(self.destination.exists())

    def snapshot(self, days_ago):
        """创建包含真实小 SQLite 的本 owner 日期快照，用于轮换边界验证。"""
        day = (datetime.date.today() - datetime.timedelta(days=days_ago)).isoformat()
        directory = self.destination / day
        with backup.private_creation():
            directory.mkdir(mode=0o700, parents=True)
            record = backup.copy_database(self.source / 'default.db', directory / 'db/default.db')
            backup.write_private_json(directory / 'verified.json', {'schema_version': 1, 'owner': backup.OWNER,
                'source_root': str(self.source), 'verified': True, 'date': day,
                'databases': [dict(record, path='db/default.db')]})
        return directory

    def test_rotation_removes_only_the_oldest_of_eight_verified_owned_snapshots(self):
        """八份真实有效自有快照只删除最旧一份，保留最新七份和删除前精确审计。"""
        self.database('default.db')
        snapshots = [self.snapshot(days) for days in range(8)]
        with mock.patch.object(backup, 'container_mount_sources', return_value=set()):
            result = backup.backup(True)
        self.assertEqual(result['removed_snapshot_count'], 1)
        self.assertFalse(snapshots[-1].exists())
        self.assertTrue(all(path.is_dir() for path in snapshots[:-1]))
        audit = list(self.destination.glob('.retention-*.json'))
        self.assertEqual(len(audit), 1)
        record = json.loads(audit[0].read_text())['candidates'][0]
        self.assertIn('db/default.db', record['identity'])
        self.assertEqual(len(record['manifest_sha256']), 64)

    def test_rotation_preserves_unknown_failed_mounted_and_damaged_backups(self):
        """未知归属、恢复来源、失败现场、受挂载引用和损坏快照全部保留。"""
        self.database('default.db')
        snapshots = [self.snapshot(days) for days in range(11)]
        unknown = snapshots[-1] / 'verified.json'
        value = json.loads(unknown.read_text())
        value['owner'] = 'another-backup-task'
        unknown.write_text(json.dumps(value))
        damaged = snapshots[-2] / 'db/default.db'
        with contextlib.closing(sqlite3.connect(str(damaged))) as connection, connection:
            connection.execute("INSERT INTO sample VALUES ('hash-changed')")
        from_full = self.destination / 'from-full-2026-11-01'
        from_full.mkdir(mode=0o700)
        failed = self.destination / ('.incomplete-' + self.day + '-protected')
        failed.mkdir(mode=0o700)
        mounted = snapshots[-3]
        with mock.patch.object(backup, 'container_mount_sources', return_value={mounted / 'db/default.db'}):
            result = backup.backup(True)
        self.assertEqual(result['removed_snapshot_count'], 1)
        self.assertEqual(result['skipped_mounted_count'], 1)
        removed = snapshots[-4]
        self.assertFalse(removed.exists())
        self.assertTrue(all(path.is_dir() for path in snapshots if path != removed))
        self.assertTrue(from_full.is_dir())
        self.assertTrue(failed.is_dir())


if __name__ == '__main__':
    unittest.main()
