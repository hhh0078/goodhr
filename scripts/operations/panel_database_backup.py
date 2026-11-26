"""文件作用：只用标准库在线备份宝塔 SQLite；默认预览，全部验证后发布当日私有目录，不删除历史备份。"""

import argparse
import contextlib
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import sqlite3
import stat
import tempfile
import time


SOURCE_ROOT = Path('/www/server/panel/data')
BACKUP_ROOT = Path('/www/backup/database/baota-panel')
LOCK_PATH = Path('/opt/goodhr-maintenance/panel-backup-retention.lock')
EXCLUDED = {'agent', 'hids_data'}
EXTENSIONS = {'.db', '.sqlite', '.sqlite3'}
OWNER = 'baota-panel-sqlite-backup'
DATABASE_TIMEOUT = 120


def require(condition, message):
    """条件不满足时停止，不将不完整目录当作成功备份。"""
    if not condition:
        raise RuntimeError(message)


def checked_path(path, directory=False):
    """只接受绝对、真实且无符号链接的既有路径，并核对类型。"""
    require(path.is_absolute() and path.resolve(strict=True) == path, '路径不安全')
    value = path.lstat()
    require(stat.S_ISDIR(value.st_mode) if directory else stat.S_ISREG(value.st_mode), '路径类型不符')
    return value


@contextlib.contextmanager
def maintenance_lock():
    """复用既有维护锁，拒绝新建、替换或跟随链接，避免与面板备份清理并发。"""
    before = checked_path(LOCK_PATH)
    descriptor = os.open(str(LOCK_PATH), os.O_RDWR | os.O_NOFOLLOW)
    try:
        opened = os.fstat(descriptor)
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino)
                and opened.st_uid == os.geteuid(), '维护锁身份不符')
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        os.close(descriptor)


@contextlib.contextmanager
def private_creation():
    """只在本轮备份内限制新文件和多级目录权限，结束后恢复进程原有 umask。"""
    previous = os.umask(0o077)
    try:
        yield
    finally:
        os.umask(previous)


def discover_databases():
    """递归收集固定来源中的 SQLite 文件，跳过两个指定子树，遇到链接或特殊数据库文件即停止。"""
    checked_path(SOURCE_ROOT, directory=True)
    require(SOURCE_ROOT != BACKUP_ROOT and SOURCE_ROOT not in BACKUP_ROOT.parents
            and BACKUP_ROOT not in SOURCE_ROOT.parents, '源与备份目录交叠')
    result = []
    for current, directories, files in os.walk(str(SOURCE_ROOT), followlinks=False):
        parent = Path(current)
        checked_path(parent, directory=True)
        directories[:] = [name for name in directories if name not in EXCLUDED]
        for name in directories:
            checked_path(parent / name, directory=True)
        for name in files:
            path = parent / name
            if path.suffix.lower() not in EXTENSIONS:
                continue
            checked_path(path)
            require(SOURCE_ROOT in path.parents, '数据库超出来源目录')
            result.append(path)
    require(result, '未找到面板数据库')
    return sorted(result)


def check_deadline(deadline):
    """为备份、校验和摘要共用每个数据库的两分钟截止时间。"""
    if time.monotonic() >= deadline:
        raise TimeoutError('数据库备份校验超时')


def readonly_connection(path):
    """通过 SQLite 只读 URI 打开源库或已完成副本，不创建或改写来源数据库。"""
    return sqlite3.connect(path.as_uri() + '?mode=ro', uri=True, timeout=1)


def verify_database(path, deadline):
    """完整验证 SQLite 副本并计算 SHA256，仅读取，不输出数据库内容。"""
    checked_path(path)
    connection = readonly_connection(path)
    try:
        connection.set_progress_handler(lambda: int(time.monotonic() >= deadline), 1000)
        require(connection.execute('PRAGMA integrity_check').fetchall() == [('ok',)], '数据库完整性未通过')
    finally:
        connection.close()
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            check_deadline(deadline)
            digest.update(block)
    check_deadline(deadline)
    return {'size_bytes': path.stat().st_size, 'sha256': digest.hexdigest()}


def fsync_directory(path):
    """同步目录项，确保文件清单和最终目录改名持久化。"""
    descriptor = os.open(str(path), os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def write_private_json(path, value):
    """独占创建 0600 清单并同步，不覆盖已有证据。"""
    descriptor = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def copy_database(source, destination):
    """用 SQLite 在线备份 API 包含 WAL 已提交内容，保留相对目录，不直接复制活动数据库文件。"""
    before = checked_path(source)
    deadline = time.monotonic() + DATABASE_TIMEOUT
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    descriptor = os.open(str(destination), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    os.close(descriptor)
    origin = readonly_connection(source)
    target = None
    try:
        target = sqlite3.connect(str(destination), timeout=1)
        # 按页回调也覆盖 SQLITE_BUSY 重试，不能因活动库占用而无限等待。
        def progress(status, remaining, total):
            """每轮备份重试都检查截止时间，不记录数据或凭据。"""
            check_deadline(deadline)

        origin.backup(target, pages=256, progress=progress, sleep=0.1)
        # 只将副本改为独立文件，避免后续只读校验再生成 WAL/SHM；来源仍保持原来的 WAL 模式。
        check_deadline(deadline)
        require(target.execute('PRAGMA journal_mode=DELETE').fetchone() == ('delete',), '副本日志模式未完成')
    finally:
        if target is not None:
            target.close()
        origin.close()
    after = checked_path(source)
    require((before.st_dev, before.st_ino) == (after.st_dev, after.st_ino), '备份期间来源被替换')
    with destination.open('rb') as stream:
        os.fsync(stream.fileno())
    return verify_database(destination, deadline)


def verify_snapshot(directory, day):
    """核对当日清单、所有副本的摘要和完整性；损坏、额外文件及越界清单均不能幂等跳过。"""
    checked_path(directory, directory=True)
    manifest_path = directory / 'verified.json'
    checked_path(manifest_path)
    require(manifest_path.stat().st_size <= 1024 * 1024, '备份清单过大')
    manifest = json.loads(manifest_path.read_text())
    require(manifest.get('schema_version') == 1 and manifest.get('owner') == OWNER
            and manifest.get('verified') is True and manifest.get('date') == day
            and manifest.get('source_root') == str(SOURCE_ROOT), '备份清单身份不符')
    records = manifest.get('databases')
    require(isinstance(records, list) and records, '备份清单为空')
    expected = {'verified.json'}
    for record in records:
        name = record.get('path', '')
        relative = PurePosixPath(name)
        require(isinstance(name, str) and name and '\\' not in name and str(relative) == name
                and not relative.is_absolute() and '..' not in relative.parts
                and not EXCLUDED.intersection(relative.parts) and relative.suffix.lower() in EXTENSIONS
                and name not in expected, '备份清单路径越界或重复')
        path = directory.joinpath(*relative.parts)
        require(directory in path.parents, '备份文件越界')
        checked_path(path)
        require(stat.S_IMODE(path.stat().st_mode) == 0o600
                and re.fullmatch(r'[0-9a-f]{64}', record.get('sha256', '')) is not None,
                '备份权限或摘要不符')
        actual = verify_database(path, time.monotonic() + DATABASE_TIMEOUT)
        require(actual['sha256'] == record['sha256'] and actual['size_bytes'] == record.get('size_bytes'), '备份摘要不符')
        expected.add(name)
    actual_files = set()
    for current, directories, files in os.walk(str(directory), followlinks=False):
        parent = Path(current)
        checked_path(parent, directory=True)
        require(stat.S_IMODE(parent.stat().st_mode) == 0o700, '备份目录权限不符')
        for name in directories:
            checked_path(parent / name, directory=True)
        for name in files:
            path = parent / name
            checked_path(path)
            actual_files.add(path.relative_to(directory).as_posix())
    require(actual_files == expected and stat.S_IMODE(manifest_path.stat().st_mode) == 0o600, '存在未核验的备份文件')
    return len(records)


def backup(apply=False):
    """默认只列数量；执行时全部校验后才改名发布，任何失败均保留临时目录，不删除历史数据库。"""
    with maintenance_lock(), private_creation():
        sources = discover_databases()
        if not apply:
            return {'success': True, 'applied': False, 'database_count': len(sources)}
        checked_path(BACKUP_ROOT.parent, directory=True)
        if not BACKUP_ROOT.exists():
            BACKUP_ROOT.mkdir(mode=0o700)
        checked_path(BACKUP_ROOT, directory=True)
        require(stat.S_IMODE(BACKUP_ROOT.stat().st_mode) == 0o700, '备份根目录必须私有')
        day = datetime.date.today().isoformat()
        final = BACKUP_ROOT / day
        if final.exists() or final.is_symlink():
            count = verify_snapshot(final, day)
            return {'success': True, 'applied': True, 'skipped_verified': True, 'database_count': count}
        temporary = Path(tempfile.mkdtemp(prefix='.incomplete-' + day + '-', dir=str(BACKUP_ROOT)))
        records = []
        try:
            for source in sources:
                relative = source.relative_to(SOURCE_ROOT)
                record = copy_database(source, temporary / relative)
                records.append(dict(record, path=relative.as_posix()))
            require(discover_databases() == sources, '备份期间数据库清单变化')
            manifest = {'schema_version': 1, 'owner': OWNER, 'date': day, 'source_root': str(SOURCE_ROOT),
                        'verified': True, 'databases': records}
            write_private_json(temporary / 'verified.json', manifest)
            verify_snapshot(temporary, day)
            for current, directories, files in os.walk(str(temporary), topdown=False):
                fsync_directory(Path(current))
            require(not final.exists() and not final.is_symlink(), '当日备份目录已出现')
            temporary.rename(final)
            fsync_directory(BACKUP_ROOT)
        except Exception as error:
            # 只记录异常类型和完成数量，失败库与现场全部保留，不输出库正文或原始错误。
            if temporary.exists():
                write_private_json(temporary / 'failed.json', {'success': False, 'error_type': type(error).__name__,
                                                              'completed_count': len(records)})
                fsync_directory(temporary)
            raise
        return {'success': True, 'applied': True, 'skipped_verified': False, 'database_count': len(records)}


def main():
    """命令行仅显式 --apply 写入，输出计数或稳定错误类型，不打印数据库和配置内容。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    try:
        print(json.dumps(backup(args.apply), ensure_ascii=False))
        return 0
    except Exception as error:
        print(json.dumps({'success': False, 'error_type': type(error).__name__}, ensure_ascii=False))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
