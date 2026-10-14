"""文件作用：仅清理宝塔每日面板ZIP备份，最多保留最近三份，不读取备份内容或触碰项目发布备份。"""

import argparse
import datetime
import fcntl
import json
import pathlib
import re
import time

BACKUP_ROOT = pathlib.Path('/www/backup/panel')
MAINTENANCE_ROOT = pathlib.Path('/opt/goodhr-maintenance')


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


def prune(apply=False):
    """保留最新三份，只删除更早且至少一小时未改动的备份，避免影响正在写入的文件。"""
    files = backup_files()
    deleted = []
    for path in files[3:]:
        if time.time() - path.stat().st_mtime < 3600:
            continue
        if apply:
            path.unlink()
        deleted.append(path.name)
    return {'applied': apply, 'keep': 3, 'retained': [path.name for path in files[:3]], 'removed': deleted}


def main():
    """用固定私有锁串行执行清理，默认仅预览，计划任务显式传入apply。"""
    parser = argparse.ArgumentParser(description='宝塔每日面板备份保留三份')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    MAINTENANCE_ROOT.mkdir(mode=0o700, exist_ok=True)
    with (MAINTENANCE_ROOT / 'panel-backup-retention.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        print(json.dumps(prune(args.apply), ensure_ascii=False))


if __name__ == '__main__':
    main()
