"""文件作用：用真实Git验证发布工具修复可激活原候选，业务输入变化、构建阶段和脏工作区仍被拒绝。"""

from pathlib import Path
import subprocess
import tempfile
import unittest
from release_activate import verify_application_source
from release_cutover import CutoverError


SCRIPT = Path(__file__).with_name("production-release.sh")


class ProductionSourceTests(unittest.TestCase):
    """只执行正式入口的源码门禁，不访问Docker、数据库或服务器。"""

    def test_tool_only_update_does_not_relabel_or_rebuild_application(self):
        """原应用提交始终固定，只有激活阶段且全部应用输入相同才接受新版发布工具。"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            (root / "cloud/backend").mkdir(parents=True)
            app = root / "cloud/backend/app.go"
            app.write_text("固定应用")
            def commit():
                """提交本测试临时库，不操作共享项目的Git状态。"""
                subprocess.run(["git", "-C", str(root), "add", "."], check=True)
                subprocess.run(["git", "-C", str(root), "-c", "user.name=测试", "-c", "user.email=test@example.invalid", "commit", "-qm", "测试提交"], check=True)
                return subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
            original = commit()
            (root / "deploy.py").write_text("仅修复发布工具")
            tools = commit()
            def check(action, tool_commit=tools):
                """通过位置参数安全加载真实正式门禁，保留原应用身份。"""
                command = 'source "$1"; action="$2"; GOODHR_SOURCE_COMMIT="$3"; production_tool_commit="$4"; production_check_source'
                result = subprocess.run(["bash", "-c", command, "source-test", str(SCRIPT), action, original, tool_commit], cwd=root, capture_output=True).returncode
                if action == "--activate-only":
                    try:
                        verify_application_source(root, original, tool_commit)
                        python_passed = True
                    except CutoverError:
                        python_passed = False
                    self.assertEqual(python_passed, result == 0)
                return result
            self.assertEqual(check("--activate-only"), 0)
            self.assertNotEqual(check("--build-only"), 0)
            app.write_text("应用改动不能沿用旧验收")
            self.assertNotEqual(check("--activate-only"), 0)
            changed = commit()
            self.assertNotEqual(check("--activate-only", changed), 0)


if __name__ == "__main__":
    unittest.main()
