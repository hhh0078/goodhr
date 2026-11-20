"""文件作用：验证地图Nginx接入范围、幂等性、备份和失败恢复，不操作本机或生产Nginx。"""

from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock

from install_static_sitemap import install, updated_config
from refresh_public_sitemap import publish_documents


SNIPPET = (Path(__file__).resolve().parents[1] / "nginx.sitemap-static.conf").read_text()
BASE = '''# GoodHR站点
server {
    listen 443 ssl;
    server_name goodhr.58it.cn;
    ssl_certificate /existing/fullchain.pem;
    add_header Strict-Transport-Security "max-age=31536000" always;
    location /api/ { proxy_pass http://127.0.0.1:8086/api/; }
    location ^~ / { proxy_pass http://127.0.0.1:5175; }
    location = /robots.txt { proxy_pass http://127.0.0.1:5175; }
}
'''


class InstallTests(unittest.TestCase):
    """InstallTests 仅允许本任务的两个地图路径发生变化。"""

    def test_only_sitemap_rules_change_and_repeat_is_idempotent(self):
        """保留SSL、robots和两个业务反代，支持旧地图规则及注释中的大括号。"""
        old = BASE.replace('    location ^~ /', '    # ignore { sitemap }\n    location = /sitemap.xml { proxy_pass http://127.0.0.1:5175; }\n    location ^~ /')
        result = updated_config(old, SNIPPET)
        self.assertEqual(result.count('location = /sitemap.xml'), 1)
        self.assertEqual(result.count('location ^~ /sitemaps/'), 1)
        self.assertIn('location = /robots.txt { proxy_pass http://127.0.0.1:5175; }', result)
        self.assertIn('ssl_certificate /existing/fullchain.pem;', result)
        self.assertIn('add_header Strict-Transport-Security "max-age=31536000" always;', result)
        self.assertEqual(updated_config(result, SNIPPET), result)

    def test_wrong_site_is_not_overwritten_and_shared_regex_is_preserved(self):
        """无法确认正式目标时停止；共享正则保持原样，新地图用更具体规则接管。"""
        for invalid in (BASE.replace('goodhr.58it.cn', 'another.invalid'), BASE.replace(':5175', ':5173')):
            with self.subTest(config=invalid), self.assertRaises(ValueError):
                updated_config(invalid, SNIPPET)
        shared = 'location ~ ^/(sitemap.xml|robots.txt)$ { return 200; }'
        result = updated_config(BASE.replace('    listen', '    ' + shared + '\n    listen'), SNIPPET)
        self.assertIn(shared, result)
        self.assertIn('location = /sitemap.xml', result)

    def test_nested_map_rules_are_removed_as_one_owned_block(self):
        """旧地图嵌套规则整体替换时不误删相邻业务配置。"""
        old = BASE.replace('    listen', '    location ^~ /sitemaps/ { location = /sitemaps/site.xml { return 200; } }\n    listen')
        result = updated_config(old, SNIPPET)
        self.assertEqual(result.count('location ^~ /sitemaps/'), 1)
        self.assertIn('location /api/ { proxy_pass http://127.0.0.1:8086/api/; }', result)

    def test_install_backup_validate_reload_and_restore_on_failure(self):
        """成功才重载；语法或重载失败则恢复原字节，重复执行不再创建备份或重载。"""
        for failure in (None, 'syntax', 'reload'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                vhost, snippet, output, backups = root / 'site.conf', root / 'snippet.conf', root / 'output', root / 'backups'
                output.mkdir()
                publish_documents(output, {'sitemap.xml': b'<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://goodhr.58it.cn</loc></url></urlset>'})
                vhost.write_text(BASE)
                snippet.write_text(SNIPPET)
                run = Mock(side_effect=[None, subprocess.CalledProcessError(1, 'nginx'), None, None] if failure == 'syntax' else
                           [None, None, subprocess.CalledProcessError(1, 'nginx'), None, None] if failure == 'reload' else None)
                if failure:
                    with self.assertRaises(subprocess.CalledProcessError):
                        install(vhost, snippet, output, backups, Path('/mock/nginx'), run)
                    self.assertEqual(vhost.read_text(), BASE)
                else:
                    self.assertTrue(install(vhost, snippet, output, backups, Path('/mock/nginx'), run))
                    self.assertEqual(run.call_args_list[-1].args[0], ['/mock/nginx', '-s', 'reload'])
                    self.assertFalse(install(vhost, snippet, output, backups, Path('/mock/nginx'), run))
                    self.assertEqual(run.call_count, 3)
                self.assertEqual(len(list(backups.glob('*.conf'))), 1)
                self.assertEqual(next(backups.glob('*.conf')).read_text(), BASE)


if __name__ == '__main__':
    unittest.main()
