"""文件作用：把现有公开站点地图完整校验后原子发布为Nginx静态文件，定时刷新已发布文章。"""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sys
import tempfile
from urllib.parse import urlsplit
from urllib.error import HTTPError
from urllib.request import HTTPRedirectHandler, ProxyHandler, build_opener
import uuid
import xml.etree.ElementTree as ET


NS = {"s": "http://www.sitemaps.org/schemas/sitemap/0.9"}
CHILD = re.compile(r"/sitemaps/(?:site\.xml|articles/[1-9][0-9]*\.xml)\Z")
GENERATION = re.compile(r"generation-[0-9a-f]{64}\Z")
MAX_XML_BYTES = 50 * 1024 * 1024


class NoRedirect(HTTPRedirectHandler):
    """NoRedirect 不让内部源请求跳到未知主机或登录页面。"""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        """redirect_request 保持重定向为失败，由本轮刷新保留旧的完整文件。"""
        return None


def read_document(opener, source, path, site):
    """read_document 只读本机Next源，校验XML类型、canonical域名及公开边界，保留原始字节。"""
    try:
        response = opener.open(source + path, timeout=10)
    except HTTPError as error:
        error.close()
        raise
    with response:
        data = response.read(MAX_XML_BYTES + 1)
    if len(data) > MAX_XML_BYTES or b"<!DOCTYPE" in data.upper() or b"<!ENTITY" in data.upper():
        raise ValueError("站点地图超过协议大小或包含不允许的XML声明")
    document = ET.fromstring(data)
    kind = document.tag.removeprefix("{" + NS["s"] + "}")
    if document.tag != "{" + NS["s"] + "}" + kind or kind not in ("urlset", "sitemapindex"):
        raise ValueError("源响应不是合法站点地图")
    item = "url" if kind == "urlset" else "sitemap"
    nodes = document.findall("s:" + item, NS)
    locations = document.findall("s:" + item + "/s:loc", NS)
    if not nodes or len(nodes) != len(locations) or len(nodes) > 50000:
        raise ValueError("站点地图条目缺少地址或超过协议数量")
    urls = []
    for node in locations:
        url = (node.text or "").strip()
        parsed = urlsplit(url)
        if parsed.scheme + "://" + parsed.netloc != site or parsed.query or parsed.fragment:
            raise ValueError("站点地图包含非canonical域名或带参数地址")
        if any(parsed.path == private or parsed.path.startswith(private + "/") for private in ("/admin", "/api", "/login")):
            raise ValueError("站点地图包含非公开地址")
        if kind == "sitemapindex" and not CHILD.fullmatch(parsed.path):
            raise ValueError("站点地图包含未知分片路径")
        urls.append(url)
    if len(urls) != len(set(urls)):
        raise ValueError("同一站点地图包含重复地址")
    return data, kind, urls


def collect_documents(source, site):
    """collect_documents 复用当前动态源的完整小站地图或分片索引，不复制文章规则或截断目录。"""
    parsed = urlsplit(source)
    if parsed.scheme != "http" or parsed.hostname not in ("127.0.0.1", "localhost", "::1") or parsed.path or parsed.query or parsed.fragment or parsed.username is not None:
        raise ValueError("地图源必须是无凭据、无路径的本机HTTP地址")
    canonical = urlsplit(site)
    if canonical.scheme != "https" or not canonical.netloc or canonical.path or canonical.query or canonical.fragment or canonical.username is not None:
        raise ValueError("canonical站点必须是HTTPS域名")
    opener = build_opener(ProxyHandler({}), NoRedirect())
    root, kind, urls = read_document(opener, source, "/sitemap.xml", site)
    paths = {"/sitemaps/site.xml"}
    if kind == "sitemapindex":
        paths.update(urlsplit(url).path for url in urls)
    elif any(urlsplit(url).path.startswith("/articles/") for url in urls):
        paths.add("/sitemaps/articles/1.xml")
    documents = {"sitemap.xml": root}
    for path in sorted(paths):
        data, child_kind, _ = read_document(opener, source, path, site)
        if child_kind != "urlset":
            raise ValueError("分片必须是URL列表，不能嵌套索引")
        documents[path.lstrip("/")] = data
    return documents


def current_generation(root):
    """current_generation 仅接受本脚本生成的相对目录链接，不覆盖未知目录或外部链接。"""
    current = root / "current"
    if not current.is_symlink():
        if current.exists():
            raise ValueError("current已被其他文件占用")
        return None
    target = os.readlink(current)
    if not GENERATION.fullmatch(target) or (root / target).is_symlink():
        raise ValueError("current不是本脚本拥有的静态缓存")
    if not (root / target / ".sitemap-generation.json").is_file():
        raise ValueError("静态缓存缺少生成记录")
    return target


def publish_documents(root, documents):
    """publish_documents 先完整落盘再原子切换目录，内容未变不改mtime，仅移除上一份自有XML缓存。"""
    previous = current_generation(root)
    hashes = {name: hashlib.sha256(data).hexdigest() for name, data in sorted(documents.items())}
    digest = hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest()
    name = "generation-" + digest
    if previous == name:
        return False
    target = root / name
    if target.exists():
        if target.is_symlink() or json.loads((target / ".sitemap-generation.json").read_text()).get("files") != hashes or any(
                (target / relative).is_symlink() or (target / relative).read_bytes() != data for relative, data in documents.items()):
            raise ValueError("已有缓存不匹配，停止覆盖")
    else:
        with tempfile.TemporaryDirectory(prefix=".sitemap-building-", dir=root) as temporary:
            stage = Path(temporary)
            for relative, data in documents.items():
                if relative != "sitemap.xml" and not CHILD.fullmatch("/" + relative):
                    raise ValueError("输出文件超出站点地图目录")
                path = stage / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
                path.chmod(0o644)
            (stage / ".sitemap-generation.json").write_text(json.dumps({"schema_version": 1, "files": hashes}))
            for path in stage.rglob("*"):
                if path.is_dir():
                    path.chmod(0o755)
            stage.chmod(0o755)
            stage.rename(target)
    link = root / (".current-" + uuid.uuid4().hex)
    try:
        link.symlink_to(name)
        link.replace(root / "current")
    except BaseException:
        if current_generation(root) != name:
            shutil.rmtree(target)
        raise
    finally:
        link.unlink(missing_ok=True)
    if previous:
        shutil.rmtree(root / previous)
    return True


def refresh(source, site, root):
    """refresh 独立锁住静态刷新，不占用发布锁；任何读取或校验失败均不替换上次完整文件。"""
    if root.is_symlink():
        raise ValueError("静态输出根目录不能是链接")
    root.mkdir(parents=True, exist_ok=True, mode=0o755)
    root.chmod(0o755)
    with (root / ".refresh.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return {"skipped": "refresh_in_progress"}
        documents = collect_documents(source.rstrip("/"), site.rstrip("/"))
        return {"changed": publish_documents(root, documents), "files": len(documents)}


def main():
    """main 供既有每分钟任务调用，不重载Nginx、不改数据库或生产容器。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", default="http://127.0.0.1:5175")
    parser.add_argument("--site", default="https://goodhr.58it.cn")
    parser.add_argument("--output", type=Path, default=Path("/www/wwwroot/goodhr-production/seo-static"))
    args = parser.parse_args()
    try:
        print(json.dumps(refresh(args.source, args.site, args.output), ensure_ascii=False))
        return 0
    except (OSError, ValueError, ET.ParseError) as error:
        print("静态站点地图本轮未更新：" + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
