"""文件作用：用纯假Docker验证已验产物迁移的配置保留、失败恢复和非目标保护，不访问真实服务器或数据库。"""

import copy
import importlib.util
import json
from pathlib import Path
import stat
from types import SimpleNamespace
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('cutover_under_test', Path(__file__).with_name('release_cutover.py'))
cutover = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cutover)
SECRET = 'synthetic-secret-must-never-appear-in-audit'


def original(role):
    """original 构造包含假秘密和完整宿主设置的原服务，测试不使用任何真实配置。"""
    identifier, image = cutover.BASELINES[role]
    name = 'goodhr-production-' + role + '-1'
    host = role == 'backend'
    network = 'host' if host else 'goodhr-production_default'
    return {'Id': identifier, 'Name': '/' + name, 'Image': image, 'RestartCount': 0,
            'State': {'Running': True, 'Status': 'running', 'StartedAt': '2026-10-14T18:00:00Z'},
            'Config': {'Hostname': 'preserved-host', 'Image': 'old-image-reference', 'Env': ['DATABASE_SECRET=' + SECRET, 'PORT=3000'], 'Cmd': ['goodhr-server'] if host else ['node', 'server.js'], 'Entrypoint': None if host else ['docker-entrypoint.sh'], 'User': '', 'WorkingDir': '/app', 'ExposedPorts': None if host else {'3000/tcp': {}}, 'Labels': {'goodhr.release': 'old-release', 'goodhr.release_kind': role, 'routing.policy': 'preserve-me'}, 'Healthcheck': None, 'OpenStdin': False, 'Tty': False},
            'HostConfig': {'NetworkMode': network, 'RestartPolicy': {'Name': 'unless-stopped', 'MaximumRetryCount': 0}, 'PortBindings': {} if host else {'3000/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '5175'}]}, 'Binds': ['/data/uploads:/app/uploads:rw'] if host else None, 'ReadonlyRootfs': False, 'Privileged': False, 'Memory': 0, 'MemorySwap': 0, 'NanoCpus': 0, 'AutoRemove': False, 'ContainerIDFile': '', 'VolumesFrom': None, 'LogConfig': {'Type': 'json-file', 'Config': {'token-like-test-option': SECRET}}},
            'Mounts': [{'Type': 'bind', 'Source': '/data/uploads', 'Destination': '/app/uploads', 'RW': True, 'Propagation': 'rprivate'}] if host else [],
            'NetworkSettings': {'Networks': {network: {} if host else {'IPAMConfig': None, 'Aliases': [identifier[:12], name], 'DriverOpts': None, 'Links': None}}}}


def spec():
    """spec 使用固定真实对象身份但完全虚构运行内容，生成可注入的安全测试计划。"""
    value = {'schema_version': 1, 'kind': 'verified-runtime-repair', 'host': cutover.HOST, 'release_id': cutover.RELEASE, 'release_root': str(cutover.ROOT), 'source_commit': '1' * 40, 'migration_fingerprint': cutover.FINGERPRINT, 'simulation_only': False, 'services': {}}
    for role in cutover.ROLES:
        name = 'goodhr-production-' + role + '-1'
        value['services'][role] = {'role': role, 'name': name, 'backup_name': name + '-backup-' + cutover.RELEASE, 'failed_name': name + '-failed-' + cutover.RELEASE, 'old_container_id': cutover.BASELINES[role][0], 'old_image': cutover.BASELINES[role][1], 'new_image': cutover.TARGETS[role], 'app_source_commit': cutover.ORIGINS[role], 'dockerfile_sha256': 'a' * 64}
    return value


class FakeEngine:
    """仅在内存改变两个目标和明确创建的对象，触碰其他容器就使测试失败。"""

    def __init__(self):
        """__init__ 准备双服务及一个不属于本次操作的运行服务。"""
        self.rows = {row['Id']: row for row in [original(role) for role in cutover.ROLES]}
        other = copy.deepcopy(original('frontend'))
        other.update(Id='f' * 64, Name='/unrelated-production-service', Image='sha256:' + 'f' * 64)
        self.rows[other['Id']] = other
        self.other = copy.deepcopy(other)
        self.mutations = []
        self.fail_create = None
        self.uncertain_create = False

    def inspect(self, reference, missing=False):
        """inspect 只返回内存深副本，防止待测代码直接修改假daemon数据。"""
        row = next((value for value in self.rows.values() if value['Id'] == reference or value['Name'].lstrip('/') == reference), None)
        if row is None and not missing:
            raise cutover.CutoverError('测试对象不存在')
        return copy.deepcopy(row)

    def snapshot(self):
        """snapshot 生成与真实引擎一致的非敏感容器身份状态字段。"""
        return sorted([{'Id': value['Id'], 'Name': value['Name'], 'Image': value['Image'], 'Running': value['State']['Running'], 'Status': value['State']['Status'], 'StartedAt': value['State']['StartedAt'], 'RestartCount': value['RestartCount']} for value in self.rows.values()], key=lambda row: row['Id'])

    def create(self, name, payload):
        """create 模拟创建失败或daemon已创建但响应丢失，不访问真实Docker。"""
        role = 'backend' if 'backend' in name else 'frontend'
        if self.fail_create == role:
            self.fail_create = None
            raise RuntimeError('synthetic create failure ' + SECRET)
        identifier = ('2' if role == 'backend' else '3') * 64
        config = {key: copy.deepcopy(value) for key, value in payload.items() if key not in ('HostConfig', 'NetworkingConfig')}
        old = original(role)
        networks = payload.get('NetworkingConfig', {}).get('EndpointsConfig', {'host': {}})
        self.rows[identifier] = {'Id': identifier, 'Name': '/' + name, 'Image': payload['Image'], 'RestartCount': 0, 'Config': config, 'HostConfig': copy.deepcopy(payload['HostConfig']), 'Mounts': old['Mounts'], 'NetworkSettings': {'Networks': copy.deepcopy(networks)}, 'State': {'Running': False, 'Status': 'created', 'StartedAt': '0001-01-01T00:00:00Z'}}
        self.mutations.append(('create', identifier))
        if self.uncertain_create:
            self.uncertain_create = False
            raise RuntimeError('synthetic lost response ' + SECRET)
        return identifier

    def controlled(self, identifier):
        """controlled 防止待测流程停止、启动或改名任何无关服务。"""
        if identifier == self.other['Id']:
            raise AssertionError('触碰了无关服务')
        return self.rows[identifier]

    def stop(self, identifier):
        """stop 模拟目标停止，保留原对象全部配置。"""
        row = self.controlled(identifier)
        self.mutations.append(('stop', identifier))
        row['State'].update(Running=False, Status='exited')

    def start(self, identifier):
        """start 模拟目标启动，同时反映StartedAt会更新。"""
        row = self.controlled(identifier)
        self.mutations.append(('start', identifier))
        row['State'].update(Running=True, Status='running', StartedAt='2026-10-15T12:00:00Z')

    def rename(self, identifier, name):
        """rename 拒绝名称冲突并保留对象ID。"""
        row = self.controlled(identifier)
        if any(value['Name'].lstrip('/') == name and value['Id'] != identifier for value in self.rows.values()):
            raise cutover.CutoverError('测试名称冲突')
        self.mutations.append(('rename', identifier))
        row['Name'] = '/' + name


class CutoverTests(unittest.TestCase):
    """验证生产关键切换边界，而不是逐行镜像实现。"""

    def runner(self, gate=None, ledger=None, health=None, fail_save_phases=()):
        """runner 注入纯假依赖，并收集所有公开审计输出检查秘密泄漏。"""
        engine, plan = FakeEngine(), spec()
        private = {'originals': {role: engine.inspect(cutover.BASELINES[role][0]) for role in cutover.ROLES}}
        plan['containers'] = engine.snapshot()
        audits = []

        def save(path, value):
            """save 只收集测试日志，指定阶段模拟持久日志写盘失败。"""
            if value.get('phase') in fail_save_phases:
                raise OSError('synthetic audit disk failure ' + SECRET)
            audits.append(copy.deepcopy(value))

        accepted = {'success': True, 'host': plan['host'], 'source_commit': plan['source_commit'], 'backend_image': cutover.TARGETS['backend'], 'frontend_image': cutover.TARGETS['frontend'], 'before_migration_fingerprint': cutover.FINGERPRINT, 'after_migration_fingerprint': cutover.FINGERPRINT}
        runner = cutover.Cutover(plan, private, engine, gate=gate or (lambda value: copy.deepcopy(accepted)), source=lambda value: None, ledger=ledger or (lambda value: cutover.FINGERPRINT), health=health or (lambda role: None), public=lambda: None, save=save)
        return runner, engine, audits

    def assert_restored(self, engine):
        """assert_restored 确认原两个ID重新正式运行，并且无关服务完全未变。"""
        for role in cutover.ROLES:
            row = engine.inspect('goodhr-production-' + role + '-1')
            self.assertEqual(row['Id'], cutover.BASELINES[role][0])
            self.assertTrue(row['State']['Running'])
        self.assertEqual(engine.rows[engine.other['Id']], engine.other)

    def test_full_runtime_preserved_and_only_audit_labels_change(self):
        """正常切换保留秘密环境、资源、网络、挂载和命令，其他业务标签不变。"""
        runner, engine, audits = self.runner()
        result = runner.activate()
        self.assertTrue(result['success'])
        for role in cutover.ROLES:
            row = engine.inspect('goodhr-production-' + role + '-1')
            old = runner.private['originals'][role]
            self.assertEqual(row['Config']['Env'], old['Config']['Env'])
            self.assertEqual(row['HostConfig'], old['HostConfig'])
            self.assertEqual(row['Config']['Cmd'], old['Config']['Cmd'])
            self.assertEqual(row['Config']['Entrypoint'], old['Config']['Entrypoint'])
            self.assertEqual(row['Config']['Labels']['routing.policy'], 'preserve-me')
            self.assertEqual(row['Config']['Labels']['goodhr.source_commit'], cutover.ORIGINS[role])
            self.assertEqual(row['Config']['Labels']['goodhr.release.id'], cutover.RELEASE)
            self.assertNotIn('goodhr.release', row['Config']['Labels'])
            backup = engine.inspect(runner.plan['services'][role]['backup_name'])
            self.assertEqual(backup['Id'], cutover.BASELINES[role][0])
            self.assertFalse(backup['State']['Running'])
        self.assertEqual(engine.rows[engine.other['Id']], engine.other)
        self.assertNotIn(SECRET, json.dumps(audits))

    def test_baseline_drift_aborts_before_any_stop(self):
        """批准后原服务重启或改名漂移时，不停止任何对象。"""
        runner, engine, _ = self.runner()
        engine.rows[cutover.BASELINES['backend'][0]]['RestartCount'] = 1
        with self.assertRaises(cutover.CutoverError):
            runner.activate()
        self.assertEqual(engine.mutations, [])

    def test_old_checkout_refuses_activation_before_any_stop(self):
        """尚未快进治理源码的旧checkout不能借用成功隔离证据开始停机。"""
        runner, engine, _ = self.runner()
        runner.source = cutover.source_baseline

        def command(args, timeout=60):
            """command 只模拟主机匹配但HEAD仍为旧9023的只读结果。"""
            return b'43.228.79.108\n' if args[0] == 'hostname' else b'9023e70cf54f1ce9deb8216e80ae6c3786ef1616\n'

        with mock.patch.object(cutover.socket, 'gethostname', return_value=cutover.HOST), mock.patch.object(cutover, 'command', side_effect=command):
            with self.assertRaises(cutover.CutoverError):
                runner.activate()
        self.assertEqual(engine.mutations, [])

    def test_source_lock_contention_blocks_entry_and_releases_container_locks(self):
        """自动更新持有同一路径源码锁时切换不能进入检查，已经取得的容器锁也会释放。"""
        descriptors, closed, entered = {}, [], []
        source_path = cutover.CHECKOUT / 'goodhr5/.release-source.lock'
        self.assertIn(source_path, cutover.LOCKS)
        fake_fcntl = SimpleNamespace(LOCK_EX=2, LOCK_NB=4)

        def opened(path, flags):
            """opened 为每个真实锁路径分配假fd，不触碰本机文件。"""
            descriptor = len(descriptors) + 20
            descriptors[descriptor] = str(path)
            return descriptor

        def flock(descriptor, flags):
            """flock 仅让共用源码锁冲突，模拟另一任务正在快进。"""
            if descriptors[descriptor] == str(source_path):
                raise BlockingIOError('synthetic source update in progress')

        fake_fcntl.flock = flock
        with mock.patch.dict('sys.modules', {'fcntl': fake_fcntl}), mock.patch.object(cutover.os, 'name', 'posix'), mock.patch.object(Path, 'resolve', lambda value: value), mock.patch.object(Path, 'is_file', return_value=True), mock.patch.object(Path, 'is_symlink', return_value=False), mock.patch.object(cutover.os, 'O_NOFOLLOW', 0, create=True), mock.patch.object(cutover.os, 'O_CLOEXEC', 0, create=True), mock.patch.object(cutover.os, 'open', side_effect=opened), mock.patch.object(cutover.os, 'fstat', return_value=SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=0)), mock.patch.object(cutover.os, 'close', side_effect=closed.append):
            with self.assertRaises(BlockingIOError):
                with cutover.deployment_locks():
                    entered.append(True)
        self.assertEqual(entered, [])
        self.assertEqual(closed, list(reversed(descriptors)))

    def test_private_environment_drift_aborts_before_any_stop(self):
        """原环境与0600备份不一致时不开始停机，也不输出变化值。"""
        runner, engine, audits = self.runner()
        engine.rows[cutover.BASELINES['backend'][0]]['Config']['Env'] = ['DATABASE_SECRET=different']
        with self.assertRaises(cutover.CutoverError) as error:
            runner.activate()
        self.assertNotIn('different', str(error.exception))
        self.assertEqual(engine.mutations, [])
        self.assertNotIn(SECRET, json.dumps(audits))

    def test_existing_address_override_keeps_complete_environment_order(self):
        """已核验的监听地址覆盖按原始数组复制，不取末项、不改其他环境。"""
        runner, engine, _ = self.runner()
        old = engine.rows[cutover.BASELINES['backend'][0]]
        old['Config']['Env'] += ['GOODHR_CLOUD_ADDR=127.0.0.1:8086', 'GOODHR_CLOUD_ADDR=:8080']
        runner.private['originals']['backend'] = copy.deepcopy(old)
        expected = copy.deepcopy(old['Config']['Env'])
        self.assertTrue(runner.activate()['success'])
        self.assertEqual(engine.inspect('goodhr-production-backend-1')['Config']['Env'], expected)

    def test_reordered_or_other_conflicting_environment_is_rejected(self):
        """端口重复项顺序变化或新增未核验冲突键必须拒绝，不泄漏原环境值。"""
        old = original('backend')
        old['Config']['Env'] += ['GOODHR_CLOUD_ADDR=127.0.0.1:8086', 'GOODHR_CLOUD_ADDR=:8080']
        item = spec()['services']['backend']
        payload = cutover.payload_for(old, item, cutover.RELEASE)
        engine = FakeEngine()
        identifier = engine.create(item['name'] + '-probe', payload)
        created = engine.inspect(identifier)
        created['Config']['Env'][-2:] = list(reversed(created['Config']['Env'][-2:]))
        with self.assertRaises(cutover.CutoverError):
            cutover.verify_created(created, old, payload, item)
        old['Config']['Env'] += ['DATABASE_SECRET=' + SECRET, 'DATABASE_SECRET=conflicting']
        with self.assertRaises(cutover.CutoverError) as failure:
            cutover.payload_for(old, item, cutover.RELEASE)
        self.assertNotIn(SECRET, str(failure.exception))

    def test_missing_evidence_and_wrong_ledger_never_mutate(self):
        """证据缺失或实时迁移变动在第一项Docker变更之前拦住。"""
        def reject(value):
            """reject 模拟retention证据或迁移检查拒绝。"""
            raise cutover.CutoverError('证据或迁移不匹配')
        for injected in ({'gate': reject}, {'ledger': reject}):
            with self.subTest(injected=list(injected)):
                runner, engine, _ = self.runner(**injected)
                with self.assertRaises(cutover.CutoverError):
                    runner.activate()
                self.assertEqual(engine.mutations, [])

    def test_successful_gate_for_other_objects_never_mutates(self):
        """另一提交、另一镜像或迁移指纹的成功结果也不能误放行本次切换。"""
        for key, replacement in [('source_commit', 'a' * 40), ('backend_image', 'sha256:' + 'a' * 64), ('frontend_image', 'sha256:' + 'b' * 64), ('host', 'another-host'), ('before_migration_fingerprint', 'a' * 64), ('after_migration_fingerprint', 'b' * 64)]:
            with self.subTest(field=key):
                runner, engine, _ = self.runner()
                accepted = runner.gate(runner.plan)
                accepted[key] = replacement
                runner.gate = lambda value: copy.deepcopy(accepted)
                with self.assertRaises(cutover.CutoverError):
                    runner.activate()
                self.assertEqual(engine.mutations, [])

    def test_changed_activation_evidence_never_mutates(self):
        """激活证据被改动时，在调用retention子进程和停止服务前拒绝。"""
        runner, engine, _ = self.runner(gate=cutover.retention_gate)
        runner.plan.update({'tools': {'retention': {'path': 'retention', 'sha256': 'a' * 64}}, 'evidence_files': [], 'activation_evidence': {'path': 'activation', 'sha256': 'a' * 64}})
        with mock.patch.object(cutover, 'digest', side_effect=lambda path: 'b' * 64 if path == 'activation' else 'a' * 64), mock.patch.object(cutover, 'command', side_effect=AssertionError('证据改变后不应启动子进程')):
            with self.assertRaises(cutover.CutoverError):
                runner.activate()
        self.assertEqual(engine.mutations, [])

    def test_create_failure_restores_both_originals_without_secret_log(self):
        """创建失败后恢复原两服务，底层带假秘密的异常不进入审计或最终错误。"""
        runner, engine, audits = self.runner()
        engine.fail_create = 'backend'
        with self.assertRaises(cutover.CutoverError) as error:
            runner.activate()
        self.assert_restored(engine)
        self.assertNotIn(SECRET, str(error.exception) + json.dumps(audits))

    def test_health_failure_restores_both_originals_and_retains_failed(self):
        """新后端健康失败也恢复前端原名，失败的新对象保留供审查。"""
        runner, engine, audits = self.runner()
        def health(role):
            """health 只让新后端失败，原后端恢复后可以通过。"""
            if role == 'backend' and engine.inspect('goodhr-production-backend-1')['Image'] == cutover.TARGETS['backend']:
                raise RuntimeError('synthetic health failure ' + SECRET)
        runner.health = health
        with self.assertRaises(cutover.CutoverError):
            runner.activate()
        self.assert_restored(engine)
        failed = engine.inspect(runner.plan['services']['backend']['failed_name'])
        self.assertEqual(failed['Image'], cutover.TARGETS['backend'])
        self.assertFalse(failed['State']['Running'])
        self.assertNotIn(SECRET, json.dumps(audits))

    def test_uncertain_create_response_is_bound_then_restored(self):
        """daemon创建成功但响应丢失时能识别并保留新对象，恢复原服务。"""
        runner, engine, _ = self.runner()
        engine.uncertain_create = True
        with self.assertRaises(cutover.CutoverError):
            runner.activate()
        self.assert_restored(engine)
        self.assertIsNotNone(engine.inspect(runner.plan['services']['backend']['failed_name']))

    def test_audit_write_failures_cannot_prevent_original_restore(self):
        """停机后、创建后及故障日志本身写盘失败时，恢复动作仍优先完成。"""
        for phase in ('old_stopped', 'old_retained', 'created', 'started', 'activation_succeeded'):
            with self.subTest(phase=phase):
                runner, engine, _ = self.runner(fail_save_phases=(phase, 'activation_failed', 'old_restored', 'failed_retained', 'automatic_rollback_succeeded'))
                with self.assertRaises(cutover.CutoverError):
                    runner.activate()
                self.assert_restored(engine)

    def test_rollback_attempts_frontend_even_if_old_backend_health_fails(self):
        """旧后端后验失败也不能使旧前端继续停在backup名称。"""
        runner, engine, _ = self.runner()
        runner.activate()
        runner.health = lambda role: (_ for _ in ()).throw(cutover.CutoverError('测试健康失败')) if role == 'backend' else None
        with self.assertRaises(cutover.CutoverError):
            runner.restore()
        self.assert_restored(engine)

    def test_gc_removed_original_refuses_before_stopping_new(self):
        """原对象被外部GC移除后不能执行这个原对象回退入口。"""
        runner, engine, _ = self.runner()
        runner.activate()
        del engine.rows[cutover.BASELINES['backend'][0]]
        before = list(engine.mutations)
        with self.assertRaises(cutover.CutoverError):
            runner.restore()
        self.assertEqual(engine.mutations, before)

    def test_simulation_plan_never_stops_services(self):
        """仅用于预测对象清单的模拟计划无法触发实际切换。"""
        runner, engine, _ = self.runner()
        runner.plan['simulation_only'] = True
        with self.assertRaises(cutover.CutoverError):
            runner.activate()
        self.assertEqual(engine.mutations, [])


if __name__ == '__main__':
    unittest.main()
