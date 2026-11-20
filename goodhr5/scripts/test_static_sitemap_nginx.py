"""文件作用：可选复用本机已有Nginx镜像，实测静态地图长度、压缩、HEAD和无重载更新。"""

import os
from pathlib import Path
import subprocess
import tempfile
from threading import Thread
import time
import unittest
import uuid
from http.server import ThreadingHTTPServer

from install_static_sitemap import updated_config
from refresh_public_sitemap import refresh
from test_refresh_public_sitemap import SITE, SourceHandler, urlset


IMAGE = os.environ.get("GOODHR_SITEMAP_NGINX_TEST_IMAGE")


@unittest.skipUnless(IMAGE, "仅显式使用已有Nginx镜像时运行，不下载依赖")
class NginxTests(unittest.TestCase):
    """NginxTests 不开放主机端口，不联网、不改生产或其他容器。"""

    def test_static_get_head_and_article_refresh(self):
        """真实Nginx提供完整响应长度，保留安全头，源文章变化后无需重载即可读取新文件。"""
        subprocess.run(["docker", "image", "inspect", IMAGE], check=True, stdout=subprocess.DEVNULL)
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            output = base / "output"
            source = ThreadingHTTPServer(("127.0.0.1", 0), SourceHandler)
            source.documents = {"/sitemap.xml": urlset("/", "/articles/a"), "/sitemaps/site.xml": urlset("/"), "/sitemaps/articles/1.xml": urlset("/articles/a")}
            thread = Thread(target=source.serve_forever, daemon=True)
            thread.start()
            origin = "http://127.0.0.1:" + str(source.server_port)
            name = "goodhr-sitemap-test-" + uuid.uuid4().hex[:12]
            try:
                refresh(origin, SITE, output)
                snippet = (Path(__file__).resolve().parents[1] / "nginx.sitemap-static.conf").read_text()
                server = '''server {
listen 8080; server_name goodhr.58it.cn;
add_header Strict-Transport-Security "max-age=31536000" always;
location /api/ { proxy_pass http://127.0.0.1:8086/api/; }
location ^~ / { proxy_pass http://127.0.0.1:5175; }
location = /robots.txt { return 200 "User-agent: *\\nDisallow: /admin\\n"; }
}'''
                config = "worker_processes 1;\nevents {worker_connections 64;}\nhttp {gzip on;gzip_types application/xml;\n" + updated_config(server, snippet) + "}\n"
                (base / "nginx.conf").write_text(config)
                # 使用容器自己的Linux文件系统，避免macOS共享挂载的符号链接缓存干扰Nginx结果。
                subprocess.run(["docker", "run", "-d", "--name", name, "--network", "none", "--entrypoint", "sh",
                                "-v", str(base) + ":/fixture:ro", IMAGE, "-c",
                                "mkdir -p /www/wwwroot/goodhr-production/seo-static && cp -a /fixture/output/. /www/wwwroot/goodhr-production/seo-static/ && exec nginx -c /fixture/nginx.conf -g 'daemon off;'"], check=True, stdout=subprocess.DEVNULL)

                def request(method, path="/sitemap.xml"):
                    """request 从容器自己的回环网卡发送原始HTTP，保留真实响应头。"""
                    data = (method + " " + path + " HTTP/1.0\r\nHost: goodhr.58it.cn\r\nAccept-Encoding: gzip\r\n\r\n").encode()
                    return subprocess.run(["docker", "exec", "-i", name, "nc", "-w", "2", "127.0.0.1", "8080"], input=data, capture_output=True, check=True).stdout

                for attempt in range(20):
                    try:
                        first = request("GET")
                        break
                    except subprocess.CalledProcessError:
                        if attempt == 19:
                            raise
                        time.sleep(.1)
                for method in ("GET", "HEAD"):
                    response = first if method == "GET" else request(method)
                    header, body = response.split(b"\r\n\r\n", 1)
                    self.assertIn(b"200 OK", header)
                    self.assertIn(b"Content-Length: " + str(len(source.documents["/sitemap.xml"])).encode(), header)
                    self.assertNotIn(b"Transfer-Encoding", header)
                    self.assertNotIn(b"Content-Encoding", header)
                    self.assertIn(b"Strict-Transport-Security: max-age=31536000", header)
                    self.assertEqual(body, b"" if method == "HEAD" else source.documents["/sitemap.xml"])
                source.documents["/sitemap.xml"] = urlset("/", "/articles/b")
                source.documents["/sitemaps/articles/1.xml"] = urlset("/articles/b")
                refresh(origin, SITE, output)
                generation = (output / "current").readlink().name
                subprocess.run(["docker", "cp", str(output / generation), name + ":/www/wwwroot/goodhr-production/seo-static/"], check=True)
                subprocess.run(["docker", "exec", name, "sh", "-c",
                                'cd /www/wwwroot/goodhr-production/seo-static && ln -s "$1" .next && mv -Tf .next current', "sh", generation], check=True)
                updated = request("GET").split(b"\r\n\r\n", 1)[1]
                self.assertEqual(updated, source.documents["/sitemap.xml"], subprocess.check_output(["docker", "logs", name], stderr=subprocess.STDOUT).decode())
                self.assertEqual(request("GET", "/sitemaps/articles/1.xml").split(b"\r\n\r\n", 1)[1], urlset("/articles/b"))
                self.assertIn(b"Disallow: /admin", request("GET", "/robots.txt"))
            finally:
                subprocess.run(["docker", "stop", "-t", "2", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                subprocess.run(["docker", "rm", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                source.shutdown()
                source.server_close()
                thread.join()


if __name__ == "__main__":
    unittest.main()
