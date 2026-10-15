"""文件作用：用临时项目、合成.env和模拟CLI验证正式发布入口，不操作真实Docker或服务器。

真实production_prepare的部署锁、磁盘和Git检查以夹具替身代替，避免测试写入/opt；这些准备逻辑需要独立验收。
其余入口函数直接复制当前源码执行，证据判断使用真实Python，Docker/健康/回收CLI均为受控替身。
"""

import json
import os
from pathlib import Path
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

import release_hygiene as hygiene


PROJECT = Path(__file__).resolve().parents[1]
SOURCE_FILES = ("scripts/deploy-environment.sh", "scripts/deploy-build.sh", "scripts/production-release.sh", "auto_deploy.sh")
BACKEND_ID, FRONTEND_ID = "sha256:" + "b" * 64, "sha256:" + "f" * 64
PREPARE_FIXTURE = '''
# 文件作用：仅测试使用的prepare替身，不访问真实发布目录、部署锁或Docker存储。
production_prepare() {
  export GOODHR_SOURCE_COMMIT="${GOODHR_SOURCE_COMMIT:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
  export GOODHR_RELEASE_IMAGE_TAG="${GOODHR_RELEASE_IMAGE_TAG:-fixture-tag}"
  export GOODHR_BACKEND_DOCKERFILE_SHA=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  export GOODHR_FRONTEND_DOCKERFILE_SHA=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
  production_release_dir="$root/release"
  mkdir -p "$production_release_dir"
}
'''
MOCK_CLI = r'''# 文件作用：只在临时目录模拟CLI并记录调用，绝不转发Docker、Git、curl或flock到真实程序。
import json,os,subprocess,sys
from pathlib import Path
tool,args=sys.argv[1],sys.argv[2:]
with open(os.environ['FIXTURE_LOG'],'a',encoding='utf-8') as stream:stream.write(json.dumps([tool]+args)+'\n')
if tool=='python3':
 if args and args[0].endswith('release_guard.py'):sys.exit(21 if os.environ.get('FIXTURE_GUARD_FAIL')=='1' else 0)
 if args and args[0].endswith(('release_hygiene.py','release_retention.py')):
  retention=args[0].endswith('release_retention.py')
  if retention and 'check' in args:
   registry=json.loads(Path(args[args.index('--registry')+1]).read_text())
   sys.exit(23 if os.environ.get('FIXTURE_PRECHECK_FAIL')=='1' or not registry.get('schema_version') else 0)
  if 'plan' in args:
   Path(args[args.index('--out')+1]).write_text('{"candidates":[]}',encoding='utf-8');print('{"targets":0}')
  elif 'apply' in args:
   if not retention and os.environ.get('FIXTURE_HYGIENE_FAIL')=='1':sys.exit(31)
   if retention and os.environ.get('FIXTURE_RETENTION_FAIL')=='1':sys.exit(32)
   print('{"success":true,"completed_actions":0}')
  sys.exit(0)
 sys.exit(subprocess.run([sys.executable]+args,stdin=sys.stdin.buffer).returncode)
if tool=='docker':
 if args[:2]==['image','ls']:
  if os.environ.get('FIXTURE_IMAGE_LIST_FAIL')=='1':sys.exit(24)
  if os.environ.get('FIXTURE_EXISTING_IMAGE') in ('both','backend' if 'backend' in args[-1] else 'frontend'):print('sha256:'+'e'*64)
  sys.exit(0)
 if args[:2]==['image','inspect']:
  role='backend' if 'backend' in args[-1] else 'frontend';image='sha256:'+('b' if role=='backend' else 'f')*64
  template=args[args.index('--format')+1] if '--format' in args else ''
  if template=='{{.Id}}':print(image)
  elif template.startswith('{'):
   import re
   values={}
   for key in re.findall(r'"([A-Za-z_]+)"\s*:',template):
    field=key.lower().replace('_','')
    if field in ('id','image','imageid'):value=image
    elif field in ('commit','sourcecommit','revision'):value='e'*40 if os.environ.get('FIXTURE_BAD_SOURCE')=='1' else os.environ['GOODHR_SOURCE_COMMIT']
    elif field in ('dockerfilesha','dockerfilesha256'):value=os.environ['GOODHR_'+role.upper()+'_DOCKERFILE_SHA']
    elif field in ('role','kind','releasekind'):value='frontend' if os.environ.get('FIXTURE_BAD_ROLE')=='1' else role
    elif field in ('release','releaseid'):value=os.environ['GOODHR_RELEASE_IMAGE_TAG']
    else:value=None
    values[key]=value
   print(json.dumps(values))
 elif args and args[0]=='exec':
  if 'pg_restore' in args:sys.stdin.buffer.read();print('1; 1259 1 TABLE public fixture fixture')
  else:sys.stdout.buffer.write(b'PGDMP synthetic fixture')
 elif 'images' in args:print('[]')
 sys.exit(0)
if tool=='git':
 state=Path(os.environ['FIXTURE_LOG']).with_suffix('.git-head')
 if args[:2]==['rev-parse','--is-inside-work-tree']:print('true')
 elif args and args[0]=='rev-parse':print((state.read_text() if state.exists() else 'a'*40) if args[-1]=='HEAD' and os.environ.get('FIXTURE_SOURCE_DRIFT')!='1' else 'b'*40)
 elif args and args[0]=='merge':state.write_text('b'*40)
 elif args and args[0]=='diff':print('cloud/backend/fixture.go')
 elif args and args[0]=='status' and os.environ.get('FIXTURE_UNTRACKED_SOURCE')=='1' and '--untracked-files=no' not in args:print('?? cloud/backend/untracked_fixture.go')
 sys.exit(0)
sys.exit(0)
'''


def find_bash():
    """find_bash 选择原生POSIX Bash或Git Bash，拒绝把Windows的WSL启动器当作测试shell。"""
    if os.name != "nt":
        return shutil.which("bash")
    candidates = []
    git = shutil.which("git")
    if git:
        candidates.append(Path(git).resolve().parents[1] / "usr/bin/bash.exe")
    for variable in ("ProgramFiles", "ProgramW6432"):
        if os.environ.get(variable):
            candidates.append(Path(os.environ[variable]) / "Git/usr/bin/bash.exe")
    return next((str(path) for path in candidates if path.is_file()), None)


def shell_path(path):
    """shell_path 将本机临时路径转换成Git Bash或POSIX Bash可访问的形式。"""
    value = str(Path(path).resolve()).replace("\\", "/")
    return "/" + value[0].lower() + value[2:] if os.name == "nt" else value


def write_fixture(path, text, executable=False):
    """write_fixture 以UTF8和LF写入临时夹具，避免Windows换行影响Bash。"""
    Path(path).write_bytes(text.encode("utf-8"))
    if executable:
        os.chmod(path, 0o700)


class EntryTests(unittest.TestCase):
    """执行真实发布入口控制流，所有外部服务操作都由临时CLI替身拦截。"""

    @classmethod
    def setUpClass(cls):
        """setUpClass 缺少Bash或必需POSIX工具时明确跳过，而不是尝试安装依赖。"""
        cls.bash = find_bash()
        if not cls.bash:
            raise unittest.SkipTest("未找到原生Bash/Git Bash，跳过入口控制流测试")
        tools = "export PATH=/usr/bin:$PATH; for tool in bash dirname date seq cp mkdir sha256sum cut; do command -v \"$tool\" >/dev/null || exit 1; done"
        probe = subprocess.run([cls.bash, "--noprofile", "--norc", "-c", tools], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
        if probe.returncode:
            raise unittest.SkipTest("Bash缺少入口测试所需的标准命令，未安装任何依赖")

    def setUp(self):
        """setUp 为每个场景建立独占临时项目，绝不读取真实业务.env。"""
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name).resolve()
        for name in ("scripts", "environments", "cloud/backend", "bin"):
            (self.home / name).mkdir(parents=True, exist_ok=True)
        for name in SOURCE_FILES:
            text = (PROJECT / name).read_text(encoding="utf-8")
            write_fixture(self.home / name, text + PREPARE_FIXTURE if name.endswith("production-release.sh") else text)
        write_fixture(self.home / "mock.py", MOCK_CLI)
        for tool in ("docker", "python3", "git", "curl", "flock"):
            command = "#!/usr/bin/env bash\n# 文件作用：仅转入本测试的CLI替身。\nexec " + shlex.quote(str(Path(sys.executable)).replace("\\", "/")) + " " + shlex.quote(str(self.home / "mock.py").replace("\\", "/")) + " " + tool + ' "$@"\n'
            write_fixture(self.home / "bin" / tool, command, executable=True)
        write_fixture(self.home / "environments/production.env", "# 文件作用：合成公开环境，不含生产域名或凭据。\nCOMPOSE_PROJECT_NAME=goodhr-production\nGOODHR_PUBLIC_BASE_URL=https://fixture.invalid\nGOODHR_CLOUD_ADDR=127.0.0.1:28086\nGOODHR_FRONTEND_PORT=25175\n")
        write_fixture(self.home / "cloud/backend/.env", "# 文件作用：仅供本测试解析的合成DSN。\nGOODHR_PG_DSN=postgres://fixture@127.0.0.1/fixture_db\n")
        self.evidence = {"stage": "isolated_validation", "success": True, "legacy_data_preserved": True,
                         "rollback_verified": True, "host": socket.gethostname(), "backend_image": BACKEND_ID,
                         "frontend_image": FRONTEND_ID, "source_commit": "a" * 40}
        self.flags = {}
        self.log = self.home / "calls.jsonl"

    def run_entry(self, action="--activate-only", evidence=True, auto=False):
        """run_entry 运行冻结的真实入口，返回退出状态及外部命令记录。"""
        write_fixture(self.home / "evidence.json", json.dumps(self.evidence))
        write_fixture(self.home / "registry.json", '{"schema_version":1}')
        env = {key: value for key, value in os.environ.items() if not key.startswith(("GOODHR_", "DEPLOY_", "FIXTURE_"))}
        env.update(FIXTURE_LOG=str(self.log), GOODHR_DATABASE="fixture_db", GOODHR_SOURCE_COMMIT="a" * 40,
                   GOODHR_RELEASE_IMAGE_TAG="fixture-tag", GOODHR_BACKEND_ENV=shell_path(self.home / "cloud/backend/.env"),
                   GOODHR_RETENTION_REGISTRY=shell_path(self.home / "registry.json"))
        env.update(self.flags)
        if evidence:
            env["GOODHR_VERIFIED_RELEASE_EVIDENCE"] = shell_path(self.home / "evidence.json")
        script = self.home / ("auto_deploy.sh" if auto else "scripts/deploy-build.sh")
        command = 'export PATH=' + shlex.quote(shell_path(self.home / "bin")) + ':/usr/bin:$PATH; exec bash ' + shlex.quote(shell_path(script))
        if not auto:
            command += " " + shlex.quote(action)
        result = subprocess.run([self.bash, "--noprofile", "--norc", "-c", command], env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=25)
        self.calls = [json.loads(line) for line in self.log.read_text(encoding="utf-8").splitlines()] if self.log.exists() else []
        self.builds = [call for call in self.calls if call[:2] == ["docker", "compose"] and "build" in call]
        self.ups = [call for call in self.calls if call[:2] == ["docker", "compose"] and "up" in call]
        return result

    def test_missing_or_wrong_evidence_never_starts(self):
        """无验收记录时禁止激活，镜像和源码证据不匹配也不启动。"""
        result = self.run_entry(evidence=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.ups, [])

    def test_wrong_commit_evidence_never_starts(self):
        """验收记录的源码提交错误时不得调用Compose up。"""
        self.evidence["source_commit"] = "e" * 40
        self.assertNotEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.ups, [])

    def test_wrong_validation_stage_never_starts(self):
        """健康结果不能冒充隔离验收阶段来激活候选。"""
        self.evidence["stage"] = "post_activation_health"
        self.assertNotEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.ups, [])

    def test_wrong_image_source_and_role_never_start(self):
        """镜像真实角色不匹配时，即使镜像来源guard通过也不得启动。"""
        self.flags["FIXTURE_BAD_ROLE"] = "1"
        self.assertNotEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.ups, [])

    def test_wrong_actual_image_commit_never_starts(self):
        """镜像真实源码提交不同于验收提交时不得启动。"""
        self.flags["FIXTURE_BAD_SOURCE"] = "1"
        self.assertNotEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.ups, [])

    def test_guard_failure_never_starts(self):
        """来源检查失败即停止，不越过guard启动服务。"""
        self.flags["FIXTURE_GUARD_FAIL"] = "1"
        self.assertNotEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.ups, [])

    def test_retention_precheck_failure_never_starts(self):
        """迁移或兼容回退预检失败时不能先启动再报错。"""
        self.flags["FIXTURE_PRECHECK_FAIL"] = "1"
        self.assertEqual(self.run_entry().returncode, 23)
        self.assertEqual(self.ups, [])

    def test_activate_only_does_not_rebuild_and_limits_services(self):
        """同一标签激活不重建，只以no-build方式更新前后端。"""
        self.assertEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.builds, [])
        self.assertEqual(len(self.ups), 1)
        self.assertEqual(self.ups[0][-2:], ["backend", "frontend"])
        self.assertIn("--no-build", self.ups[0])
        self.assertIn("--no-deps", self.ups[0])

    def test_existing_either_image_tag_never_rebuilds(self):
        """任意一端版本标签已存在时拒绝覆盖，不能先重建另一端。"""
        for role in ("backend", "frontend"):
            self.flags["FIXTURE_EXISTING_IMAGE"] = role
            self.assertNotEqual(self.run_entry(action="--build-only").returncode, 0)
            self.assertEqual(self.builds, [])
            self.assertEqual(self.ups, [])

    def test_image_list_failure_never_builds(self):
        """无法查询现有标签时停止，不能把Docker故障当成新标签。"""
        self.flags["FIXTURE_IMAGE_LIST_FAIL"] = "1"
        self.assertEqual(self.run_entry(action="--build-only").returncode, 24)
        self.assertEqual(self.builds, [])
        self.assertEqual(self.ups, [])

    def test_activation_accepts_existing_frozen_tags(self):
        """激活阶段应复用已经验证的标签，不会误触发构建前防覆盖检查。"""
        self.flags["FIXTURE_EXISTING_IMAGE"] = "both"
        self.assertEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.builds, [])

    def test_changed_source_after_prepare_never_activates(self):
        """准备后的真实HEAD漂移必须在镜像复核阶段阻断激活。"""
        self.flags["FIXTURE_SOURCE_DRIFT"] = "1"
        self.assertNotEqual(self.run_entry().returncode, 0)
        self.assertEqual(self.ups, [])

    def test_hygiene_failure_reports_pending_and_does_not_continue_gc(self):
        """健康后缓存收尾失败必须说明线上已激活，且不能继续Docker GC。"""
        self.flags["FIXTURE_HYGIENE_FAIL"] = "1"
        result = self.run_entry()
        self.assertEqual(result.returncode, 31)
        self.assertEqual(len(self.ups), 1)
        self.assertIn(b"cleanup_pending", result.stderr)
        self.assertEqual(len([call for call in self.calls if call[0] == "curl"]), 2)
        self.assertFalse(any(call[0] == "python3" and call[1].endswith("release_retention.py") and ("plan" in call or "apply" in call) for call in self.calls))

    def test_changed_retention_plan_reports_pending_after_activation(self):
        """回收计划变化时停止删除，保持已经通过的激活健康事实并提示待收尾。"""
        self.flags["FIXTURE_RETENTION_FAIL"] = "1"
        result = self.run_entry()
        self.assertEqual(result.returncode, 32)
        self.assertEqual(len(self.ups), 1)
        self.assertIn(b"cleanup_pending", result.stderr)

    def test_auto_deploy_only_prepares_candidate(self):
        """自动入口只构建候选，未提供验收记录也不会激活。"""
        self.assertEqual(self.run_entry(evidence=False, auto=True).returncode, 0)
        self.assertEqual(len(self.builds), 1)
        self.assertEqual(self.ups, [])

    def test_auto_deploy_rejects_global_prune(self):
        """旧全局prune开关在任何外部CLI操作前被拒绝。"""
        self.flags["DEPLOY_PRUNE"] = "1"
        self.assertNotEqual(self.run_entry(auto=True).returncode, 0)
        self.assertEqual(self.calls, [])

    def test_auto_deploy_rejects_untracked_source(self):
        """未跟踪源码仍属于未完成改动，不能被打包后伪装成HEAD提交。"""
        self.flags["FIXTURE_UNTRACKED_SOURCE"] = "1"
        self.assertNotEqual(self.run_entry(auto=True).returncode, 0)
        self.assertEqual(self.builds, [])
        self.assertEqual(self.ups, [])

    def test_zero_target_plan_is_successful(self):
        """零目标收尾计划是正常成功，不产生伪失败或跳过兼容预检。"""
        result = self.run_entry()
        self.assertEqual(result.returncode, 0)
        self.assertIn(b'"targets":0', result.stdout)
        self.assertNotIn(b"cleanup_pending", result.stderr)
        checks = [call for call in self.calls if call[0] == "python3" and call[1].endswith("release_retention.py") and "check" in call]
        self.assertEqual(len(checks), 1)


class FailedCacheTests(unittest.TestCase):
    """直接检验失败发布的缓存收尾，独立于Bash可用性且不读取任何真实Docker元数据。"""

    def test_failed_release_cleans_only_finished_cache(self):
        """失败发布只清理已完成缓存，PG排错现场和原始备份完整保留。"""
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory).resolve()
            release = base / "failed-release"
            cache = release / "frontend-build/node_modules"
            cache.mkdir(parents=True)
            (cache / "cache.bin").write_bytes(b"rebuildable")
            pg = release / "rehearsal-pgdata"
            pg.mkdir()
            (pg / "PG_VERSION").write_bytes(b"16")
            backup = release / "database.dump"
            backup.write_bytes(b"PGDMP original")
            manifest = {"schema_version": 1, "host": socket.gethostname(), "release_dir": str(release), "status": "failed",
                        "completed_workdirs": ["frontend-build/node_modules"], "isolated_postgres": [{"path": "rehearsal-pgdata", "stopped": False}]}
            write_fixture(release / hygiene.MANIFEST_NAME, json.dumps(manifest))
            reader = Mock()
            reader.containers.return_value, reader.mounts.return_value = [], []
            with patch.object(hygiene, "RELEASE_ROOT", base), patch.object(hygiene.shutil.rmtree, "avoids_symlink_attacks", True, create=True):
                plan = hygiene.create_plan(release, reader)
                path = base / "plan.json"
                hygiene.write_json(path, plan)
                result = hygiene.apply_plan(path, hygiene.file_sha(path), reader)
            self.assertEqual(result["deleted"], [str(cache)])
            self.assertTrue((pg / "PG_VERSION").exists())
            self.assertEqual(backup.read_bytes(), b"PGDMP original")
            reader.dump_toc.assert_not_called()


if __name__ == "__main__":
    unittest.main()
