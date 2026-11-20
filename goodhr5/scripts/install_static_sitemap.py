"""文件作用：自动接入GoodHR静态地图的Nginx规则，限定目标站点，备份后校验并在失败时恢复。"""

import argparse
import fcntl
import hashlib
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys
import tempfile

from refresh_public_sitemap import NS, current_generation
import xml.etree.ElementTree as ET


BEGIN = "# BEGIN GOODHR STATIC SITEMAP"
END = "# END GOODHR STATIC SITEMAP"


def nginx_structure(text):
    """nginx_structure 忽略注释和引号内的括号，定位实际配置块，不重排其他配置。"""
    masked = list(text)
    quote, comment, escaped = None, False, False
    stack, pairs = [], {}
    for index, character in enumerate(text):
        if comment:
            if character == "\n":
                comment = False
            else:
                masked[index] = " "
            continue
        if quote:
            masked[index] = " "
            if escaped:
                escaped = False
            elif character == "\\":
                escaped = True
            elif character == quote:
                quote = None
            continue
        if character == "#":
            comment = True
            masked[index] = " "
        elif character in ("'", '"'):
            quote = character
            masked[index] = " "
        elif character == "{":
            stack.append(index)
        elif character == "}":
            if not stack:
                raise ValueError("站点配置括号不匹配")
            pairs[stack.pop()] = index
    if quote or stack:
        raise ValueError("站点配置结构不完整")
    return "".join(masked), pairs


def target_server(text):
    """target_server 只选择同时绑定正式域名和既有5175/8086代理的唯一server。"""
    masked, pairs = nginx_structure(text)
    targets = []
    for match in re.finditer(r"\bserver\s*\{", masked):
        opening = match.end() - 1
        body = masked[opening:pairs[opening]]
        names = re.search(r"\bserver_name\s+([^;]+);", body)
        if names and "goodhr.58it.cn" in names[1].split() and all(value in body for value in (
                "proxy_pass http://127.0.0.1:5175;", "proxy_pass http://127.0.0.1:8086/api/;")):
            targets.append((opening, pairs[opening]))
    if len(targets) != 1:
        raise ValueError("正式域名或原代理目标无法唯一确认，停止修改")
    return targets[0], masked, pairs


def updated_config(original, snippet):
    """updated_config 只替换地图专用location及本脚本标记，不触碰域名、SSL、robots或业务代理。"""
    if original.count(BEGIN) != original.count(END) or original.count(BEGIN) > 1:
        raise ValueError("已有静态地图标记不完整")
    text = original
    if BEGIN in text:
        start, end = text.index(BEGIN), text.index(END) + len(END)
        (server_start, server_end), _, _ = target_server(original)
        if not server_start < start < end < server_end:
            raise ValueError("已有静态地图标记顺序异常")
        text = text[:start] + text[end:]
    (start, end), masked, pairs = target_server(text)
    removals = []
    for match in re.finditer(r"\blocation\s+[^{};]+\{", masked):
        opening = match.end() - 1
        if not start < opening < end:
            continue
        tokens = shlex.split(text[match.start():opening])
        selector = tokens[-1]
        # 新增的精确和^~规则优先于正则；保留共享正则，避免改变robots等其他路径。
        if len(tokens) > 2 and tokens[1] in ("~", "~*"):
            continue
        if selector == "/sitemap.xml" or selector.startswith("/sitemaps/") or selector.startswith("^/sitemaps/"):
            removals.append((match.start(), pairs[opening] + 1))
    outer = []
    for left, right in sorted(removals):
        if not outer or left >= outer[-1][1]:
            outer.append((left, right))
    for left, right in reversed(outer):
        text = text[:left] + text[right:]
    (_, closing), _, _ = target_server(text)
    block = BEGIN + "\n" + snippet.strip() + "\n" + END
    return text[:closing].rstrip() + "\n\n    " + block.replace("\n", "\n    ") + "\n" + text[closing:]


def atomic_write(path, data, metadata):
    """atomic_write 同目录原子替换，保留原站点配置的所有者和权限。"""
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".sitemap-config-", delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, stat.S_IMODE(metadata.st_mode))
        os.chown(temporary, metadata.st_uid, metadata.st_gid)
        temporary.replace(path)
    finally:
        if temporary:
            temporary.unlink(missing_ok=True)


def install(vhost, snippet, output, backups, nginx, run=subprocess.run):
    """install 先确认完整地图，再备份与语法检查；失败恢复原文，成功才平滑重载Nginx。"""
    if vhost.is_symlink() or not vhost.is_file() or not current_generation(output):
        raise ValueError("站点配置或完整静态地图尚未准备好")
    document = ET.fromstring((output / "current/sitemap.xml").read_bytes())
    if document.tag not in ("{" + NS["s"] + "}urlset", "{" + NS["s"] + "}sitemapindex"):
        raise ValueError("当前静态文件不是站点地图")
    original = vhost.read_bytes()
    candidate = updated_config(original.decode(), snippet.read_text()).encode()
    if candidate == original:
        return False
    command = [str(nginx), "-t"]
    run(command, check=True, capture_output=True)
    metadata = vhost.stat()
    backups.mkdir(parents=True, exist_ok=True, mode=0o700)
    backup = backups / (hashlib.sha256(original).hexdigest() + ".conf")
    if backup.exists() and backup.read_bytes() != original:
        raise ValueError("既有配置备份摘要不符，停止覆盖")
    if not backup.exists():
        with backup.open("xb") as stream:
            stream.write(original)
        backup.chmod(0o600)
    if vhost.read_bytes() != original:
        raise ValueError("配置在备份期间变化，停止覆盖")
    atomic_write(vhost, candidate, metadata)
    try:
        run(command, check=True, capture_output=True)
        run([str(nginx), "-s", "reload"], check=True, capture_output=True)
    except BaseException:
        if vhost.read_bytes() != candidate:
            raise RuntimeError("校验期间配置被其他任务修改，保留备份并停止覆盖")
        atomic_write(vhost, original, metadata)
        run(command, check=True, capture_output=True)
        run([str(nginx), "-s", "reload"], check=True, capture_output=True)
        raise
    return True


def main():
    """main 由原自动任务调用，只管理正式GoodHR站点的地图路径。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vhost", type=Path, default=Path("/www/server/panel/vhost/nginx/goodhr.58it.cn.conf"))
    parser.add_argument("--output", type=Path, default=Path("/www/wwwroot/goodhr-production/seo-static"))
    parser.add_argument("--backups", type=Path, default=Path("/opt/goodhr-release/sitemap-config-backups"))
    parser.add_argument("--nginx", type=Path, default=Path("/www/server/nginx/sbin/nginx"))
    args = parser.parse_args()
    try:
        args.backups.mkdir(parents=True, exist_ok=True, mode=0o700)
        with (args.backups / ".install.lock").open("a") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                print("静态地图规则正在更新，本轮不重复执行")
                return 0
            changed = install(args.vhost, Path(__file__).resolve().parents[1] / "nginx.sitemap-static.conf", args.output, args.backups, args.nginx)
        print("静态地图Nginx规则" + ("已更新并通过检查" if changed else "已生效，无需重复重载"))
        return 0
    except (OSError, ValueError, RuntimeError, ET.ParseError, subprocess.CalledProcessError) as error:
        print("静态地图规则未更新：" + type(error).__name__, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
