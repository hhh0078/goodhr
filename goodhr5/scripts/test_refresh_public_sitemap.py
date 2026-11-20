"""文件作用：用本机HTTP源验证静态地图完整更新、分片、撤回、失败保留及静态响应长度。"""

from http.server import BaseHTTPRequestHandler, SimpleHTTPRequestHandler, ThreadingHTTPServer
from functools import partial
from pathlib import Path
import tempfile
from threading import Thread
import unittest
from urllib.error import HTTPError
from urllib.request import urlopen
from unittest.mock import patch

from refresh_public_sitemap import collect_documents, refresh


SITE = "https://goodhr.58it.cn"
NAMESPACE = ' xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"'


def urlset(*paths):
    """urlset 组装仅用于测试的canonical地址，不连接生产接口或发布真实文章。"""
    return ('<?xml version="1.0"?><urlset' + NAMESPACE + '>' + ''.join('<url><loc>' + SITE + p + '</loc></url>' for p in paths) + '</urlset>').encode()


class SourceHandler(BaseHTTPRequestHandler):
    """SourceHandler 使用内存字典模拟现有Next源，允许测试发布、撤回及服务失败。"""

    def do_GET(self):
        """do_GET 返回本测试准备的XML或错误，不涉及数据库、认证或实际网站。"""
        data = self.server.documents.get(self.path)
        self.send_response(200 if data is not None else 503)
        self.end_headers()
        self.wfile.write(data or b"unavailable")

    def log_message(self, *_):
        """log_message 避免把测试HTTP访问混进发布日志。"""


class StaticSitemapTests(unittest.TestCase):
    """StaticSitemapTests 覆盖从源XML到只读静态文件的完整流程和失败边界。"""

    def setUp(self):
        """setUp 创建独立临时目录和无凭据本机HTTP源。"""
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name) / "static"
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), SourceHandler)
        self.server.documents = {"/sitemap.xml": urlset("/", "/articles/a"), "/sitemaps/site.xml": urlset("/"), "/sitemaps/articles/1.xml": urlset("/articles/a")}
        self.thread = Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.source = "http://127.0.0.1:" + str(self.server.server_port)

    def tearDown(self):
        """tearDown 结束本测试服务并移除临时文件，不接触生产快照。"""
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.directory.cleanup()

    def run_refresh(self):
        """run_refresh 调用真实生成器，所有读写限定到本机测试资源。"""
        return refresh(self.source, SITE, self.root)

    def test_publish_new_article_and_remove_withdrawn_article_without_redeploy(self):
        """源公开目录变化后下一轮加入新文章并移除撤回文章，未变更时保持mtime及缓存数量。"""
        self.assertTrue(self.run_refresh()["changed"])
        first = (self.root / "current").resolve()
        mtime = (first / "sitemap.xml").stat().st_mtime_ns
        self.assertFalse(self.run_refresh()["changed"])
        self.assertEqual((first / "sitemap.xml").stat().st_mtime_ns, mtime)
        self.server.documents["/sitemap.xml"] = urlset("/", "/articles/b")
        self.server.documents["/sitemaps/articles/1.xml"] = urlset("/articles/b")
        self.assertTrue(self.run_refresh()["changed"])
        self.assertEqual((self.root / "current/sitemap.xml").read_bytes(), urlset("/", "/articles/b"))
        self.assertNotIn(b"/articles/a", (self.root / "current/sitemaps/articles/1.xml").read_bytes())
        self.assertFalse(first.exists())
        self.assertEqual(len(list(self.root.glob("generation-*"))), 1)

    def test_index_fetches_all_shards_and_keeps_exact_xml(self):
        """切换分片索引后完整获取所有分片，保留原有XML字节、lastmod和语言标记。"""
        index = ('<sitemapindex' + NAMESPACE + '><sitemap><loc>' + SITE + '/sitemaps/articles/1.xml</loc></sitemap><sitemap><loc>' + SITE + '/sitemaps/articles/2.xml</loc></sitemap></sitemapindex>').encode()
        self.server.documents["/sitemap.xml"] = index
        self.server.documents["/sitemaps/articles/2.xml"] = urlset("/articles/b").replace(b"<url>", b'<url xmlns:xhtml="http://www.w3.org/1999/xhtml">').replace(b"</url>", b'<lastmod>2026-11-20T00:00:00Z</lastmod><xhtml:link rel="alternate" hreflang="en" href="https://goodhr.58it.cn/en/articles/b"/></url>')
        documents = collect_documents(self.source, SITE)
        self.assertEqual(len(documents), 4)
        self.assertEqual(documents["sitemap.xml"], index)
        self.assertEqual(documents["sitemaps/articles/2.xml"], self.server.documents["/sitemaps/articles/2.xml"])

    def test_upstream_failure_or_invalid_xml_keeps_whole_previous_generation(self):
        """主文档或任一分片失败时保留完整旧版本，不发布缺页、HTML错误或非公开地址。"""
        self.run_refresh()
        before = (self.root / "current").readlink()
        for invalid in (None, b"<html>bad</html>", urlset(), urlset("/admin"), urlset("/?private=1"), urlset("/").replace(SITE.encode(), b"https://wrong.invalid")):
            with self.subTest(invalid=invalid):
                self.server.documents["/sitemap.xml"] = invalid
                with self.assertRaises((ValueError, HTTPError)):
                    self.run_refresh()
                self.assertEqual((self.root / "current").readlink(), before)
        self.server.documents["/sitemap.xml"] = urlset("/", "/articles/b")
        self.server.documents["/sitemaps/articles/1.xml"] = None
        with self.assertRaises(HTTPError):
            self.run_refresh()
        self.assertEqual((self.root / "current").readlink(), before)

    def test_failed_switch_does_not_replace_current_or_remove_old_cache(self):
        """原子切换失败时当前版本及原文件仍可读取。"""
        self.run_refresh()
        before = (self.root / "current").resolve()
        self.server.documents["/sitemap.xml"] = urlset("/", "/features")
        with patch.object(Path, "replace", side_effect=OSError("模拟磁盘错误")), self.assertRaises(OSError):
            self.run_refresh()
        self.assertEqual((self.root / "current").resolve(), before)
        self.assertTrue((before / "sitemap.xml").is_file())
        self.assertTrue(self.run_refresh()["changed"])

    def test_local_static_response_has_exact_length_for_get_and_head(self):
        """实际从静态文件读取GET与HEAD，长度等于XML字节数；Nginx上线验证另行进行。"""
        self.run_refresh()
        handler = partial(SimpleHTTPRequestHandler, directory=str(self.root / "current"))
        static = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        thread = Thread(target=static.serve_forever, daemon=True)
        thread.start()
        try:
            from urllib.request import Request
            for method in ("GET", "HEAD"):
                with urlopen(Request("http://127.0.0.1:" + str(static.server_port) + "/sitemap.xml", method=method)) as response:
                    self.assertEqual(int(response.headers["Content-Length"]), len(self.server.documents["/sitemap.xml"]))
                    self.assertIsNone(response.headers["Transfer-Encoding"])
                    self.assertEqual(response.read(), b"" if method == "HEAD" else self.server.documents["/sitemap.xml"])
        finally:
            static.shutdown()
            static.server_close()
            thread.join()

    def test_unknown_output_link_and_remote_source_are_not_followed(self):
        """不改写不属于本脚本的目录，也不把公网地址或凭据当成本机源。"""
        for source in ("https://goodhr.58it.cn", "http://127.0.0.1/path", "http://:secret@127.0.0.1"):
            with self.subTest(source=source), self.assertRaises(ValueError):
                collect_documents(source, SITE)
        self.root.mkdir()
        (self.root / "current").symlink_to("../foreign")
        with self.assertRaises(ValueError):
            self.run_refresh()
        self.assertEqual((self.root / "current").readlink(), Path("../foreign"))


if __name__ == "__main__":
    unittest.main()
