"""文件作用：仅清理宝塔每日面板ZIP备份，保留最新一份并保护容器挂载，不触碰项目发布备份。"""

import argparse
import datetime
import fcntl
import json
import pathlib
import re
import subprocess
import time

BACKUP_ROOT = pathlib.Path('/www/backup/panel')
MAINTENANCE_ROOT = pathlib.Path('/opt/goodhr-maintenance')
KEEP_COUNT = 1


def backup_files():
    """只接受固定备份目录下合法日期命名的普通ZIP文件，拒绝符号链接和越界路径。"""
    if BACKUP_ROOT.is_symlink() or BACKUP_ROOT.resolve() != BACKUP_ROOT:
        raise RuntimeError('面板备份目录身份不一致')
    result = []
    for path in BACKUP_ROOT.glob('*.zip'):
        if path.is_symlink() or not path.is_file() or path.resolve().parent != BACKUP_ROOT:
            continue
        if not re.fullmatch(r'\d{4}-\d{2}-\d{2}\.zip', path.name):
            continue
        try:
            datetime.date.fromisoformat(path.stem)
        except ValueError:
            continue
        result.append(path)
    return sorted(result, key=lambda path: path.name, reverse=True)


def container_mount_sources():
    """查询全部运行及停止容器的挂载源，检查失败或结果不完整时停止清理。"""
    result = subprocess.run(['docker', 'ps', '--all', '--quiet', '--no-trunc'],
                            check=True, capture_output=True, text=True, timeout=30)
    ids = result.stdout.split()
    if not all(re.fullmatch(r'[0-9a-f]{64}', value) for value in ids):
        raise RuntimeError('容器列表格式异常，停止清理')
    if not ids:
        return set()
    result = subprocess.run(['docker', 'inspect', '--format', '{{json .Mounts}}'] + ids,
                            check=True, capture_output=True, text=True, timeout=30)
    rows = [json.loads(line) for line in result.stdout.splitlines()]
    if len(rows) != len(ids) or not all(isinstance(row, list) for row in rows):
        raise RuntimeError('容器挂载检查不完整，停止清理')
    sources = set()
    for row in rows:
        for mount in row:
            if not isinstance(mount, dict):
                raise RuntimeError('容器挂载格式异常，停止清理')
            source = mount.get('Source', '')
            if not source and mount.get('Type') == 'tmpfs':
                continue
            if not isinstance(source, str) or not pathlib.Path(source).is_absolute():
                raise RuntimeError('容器挂载源异常，停止清理')
            path = pathlib.Path(source)
            sources.update((path, path.resolve()))
    return sources


def prune(apply=False):
    """保留最新一份，仅删除一小时未改动且与全部容器挂载均不重叠的旧备份。"""
    files = backup_files()
    sources = container_mount_sources()
    deleted = []
    mounted = []
    for path in files[KEEP_COUNT:]:
        if time.time() - path.stat().st_mtime < 3600:
            continue
        if any(path == source or path in source.parents or source in path.parents for source in sources):
            mounted.append(path.name)
            continue
        if path.is_symlink() or path.resolve().parent != BACKUP_ROOT:
            raise RuntimeError('备份路径发生变化，停止清理')
        if apply:
            path.unlink()
        deleted.append(path.name)
    return {'applied': apply, 'keep': KEEP_COUNT, 'retained': [path.name for path in files[:KEEP_COUNT]],
            'removed': deleted, 'skipped_mounted': mounted}


def main():
    """用固定私有锁串行执行清理，默认仅预览，计划任务显式传入apply。"""
    parser = argparse.ArgumentParser(description='宝塔每日面板备份保留一份，跳过容器挂载')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    MAINTENANCE_ROOT.mkdir(mode=0o700, exist_ok=True)
    with (MAINTENANCE_ROOT / 'panel-backup-retention.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        print(json.dumps(prune(args.apply), ensure_ascii=False))


if __name__ == '__main__':
    main()
