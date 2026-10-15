"""文件作用：为已经验证的编译产物迁移干净运行镜像，按固定计划切换和回退双服务；不构建应用、不清理旧对象、不写数据库。"""

import argparse
from contextlib import contextmanager
import copy
import datetime as dt
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


ROOT = Path('/opt/goodhr-release/clean-runtime-20261015')
CHECKOUT = Path('/www/wwwroot/goodhr-production/good-hr-ai-js')
HOST = 'ldclouda31539'
RELEASE = 'clean-runtime-20261015'
FINGERPRINT = '4ed4cdafe3af54d1f4ee178294148039dd07163c37dff7e791231db1bd409d4e'
ROLES = ('backend', 'frontend')
LOCKS = (Path('/opt/goodhr-release/.backend-deploy.lock'), Path('/opt/goodhr-release/.frontend-deploy.lock'), Path('/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock'))
BASELINES = {
    'backend': ('f308ae790c1e6daf43e3453dd27504f7871b2cf6f1da564d95d368836646130a', 'sha256:31df9fe839f4375e0bada14816c695662029e6575d44cbac01bc2d728ce4014b'),
    'frontend': ('8d291e59f0f9d6ba01783a492efa71bda17323954a13a462b5eba1eea1eee617', 'sha256:1243f405e9a4ccbc110d8a55f2ab5dd0863dd6ad175f7784849fee88b20cf59a'),
}
TARGETS = {'backend': 'sha256:c40e79cc463b641e746d3aac3866471d02d2a0ae8e2ab2f7dcb29c451bd25102', 'frontend': 'sha256:eea3fcfadfc1ee9ec81162132d87107d2896f52445fba2b51de6028515535084'}
ORIGINS = {'backend': '3a36f424c3bf43a5ee544cba8be78ff47ef90a1b', 'frontend': '9374fd0a8332d6d07273d622816b4f0979be516f'}
HEX = re.compile(r'[0-9a-f]{64}\Z')
COMMIT = re.compile(r'[0-9a-f]{40}\Z')
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


class CutoverError(RuntimeError):
    """表示运行镜像迁移的身份、配置、证据或健康条件不满足。"""


def require(condition, message):
    """require 用固定非敏感说明终止不符合批准范围的操作。"""
    if not condition:
        raise CutoverError(message)


def digest(path):
    """digest 以文件完整字节校验批准计划和证明，不输出文件正文。"""
    result = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(block)
    return result.hexdigest()


def read_json(path):
    """read_json 读取对象格式的任务文件，拒绝链接和未知顶层类型。"""
    path = Path(path)
    require(path.is_file() and not path.is_symlink(), '任务文件不存在或为链接')
    try:
        result = json.loads(path.read_text(encoding='utf-8-sig'))
    except (ValueError, UnicodeError):
        raise CutoverError('任务JSON无法读取') from None
    require(isinstance(result, dict), '任务JSON必须为对象')
    return result


def save_json(path, value, exclusive=False):
    """save_json 只写本次发布目录的0600文件；私密原配置只留服务器，不进入输出。"""
    path = Path(path)
    root = ROOT.resolve()
    require(root == ROOT and os.path.commonpath((str(path.resolve()), str(root))) == str(root), '任务输出路径越界')
    require(path.parent.is_dir() and not path.is_symlink(), '任务输出父目录或链接异常')
    temporary = path if exclusive else path.with_name(path.name + '.tmp')
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, 'O_NOFOLLOW', 0)
    descriptor = os.open(str(temporary), flags, 0o600)
    with os.fdopen(descriptor, 'w', encoding='utf-8') as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.flush()
        os.fsync(stream.fileno())
    if not exclusive:
        temporary.replace(path)


def command(args, timeout=60):
    """command 执行受控只读工具，不把stdout、stderr或参数拼进失败日志。"""
    try:
        result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        raise CutoverError('只读检查工具不可用或超时') from None
    require(result.returncode == 0, '只读检查工具返回失败')
    return result.stdout


@contextmanager
def deployment_locks():
    """deployment_locks 同时持有三把已有root普通锁，禁止创建新锁或跟随链接。"""
    require(os.name == 'posix', '实际切换只允许在指定Linux服务器执行')
    import fcntl
    descriptors = []
    try:
        for path in sorted(LOCKS):
            require(path.resolve() == path and path.is_file() and not path.is_symlink(), '既有部署锁缺失或路径异常')
            descriptor = os.open(str(path), os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            descriptors.append(descriptor)
            info = os.fstat(descriptor)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0, '部署锁必须归root所有')
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


class DockerConnection(http.client.HTTPConnection):
    """仅连接本机Docker套接字，含环境的请求和响应始终留在内存。"""

    def connect(self):
        """connect 连接固定Unix套接字，不访问任何远端Docker服务。"""
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect('/var/run/docker.sock')


class DockerEngine:
    """仅提供本次切换所需的inspect、create、start、stop、rename，不提供删除或构建入口。"""

    def __init__(self):
        """__init__ 读取daemon API版本，不读取认证环境。"""
        version = self.request('GET', '/version')
        require(re.fullmatch(r'\d+\.\d+', version.get('ApiVersion', '')), 'Docker API版本不可确认')
        self.prefix = '/v' + version['ApiVersion']

    def request(self, method, path, payload=None, missing=False):
        """request 对Docker状态码作明确检查，错误正文和私密参数不进入异常。"""
        connection = DockerConnection('localhost', timeout=45)
        try:
            body = None if payload is None else json.dumps(payload).encode('utf-8')
            connection.request(method, path, body, {'Content-Type': 'application/json'})
            response = connection.getresponse()
            raw = response.read()
            if missing and response.status == 404:
                return None
            require(response.status in (200, 201, 204, 304), 'Docker操作状态不符合预期')
            return json.loads(raw) if raw else None
        except (OSError, ValueError, http.client.HTTPException):
            raise CutoverError('Docker操作结果不可确认') from None
        finally:
            connection.close()

    def inspect(self, reference, missing=False):
        """inspect 仅在内存读取指定目标完整设置，不向日志返回配置正文。"""
        require(re.fullmatch(r'[A-Za-z0-9_.-]+', reference), '容器引用格式异常')
        return self.request('GET', self.prefix + '/containers/' + reference + '/json', missing=missing)

    def snapshot(self):
        """snapshot 对所有容器只读取身份和状态，记录其他服务需要保持的安全基线。"""
        ids = sorted(command(['docker', 'container', 'ls', '-aq', '--no-trunc']).decode().split())
        template = '{"Id":{{json .Id}},"Name":{{json .Name}},"Image":{{json .Image}},"Running":{{json .State.Running}},"Status":{{json .State.Status}},"StartedAt":{{json .State.StartedAt}},"RestartCount":{{json .RestartCount}}}'
        rows = [json.loads(line) for line in command(['docker', 'container', 'inspect', '--format', template] + ids).decode().splitlines()] if ids else []
        require(ids == sorted(command(['docker', 'container', 'ls', '-aq', '--no-trunc']).decode().split()), '读取期间容器对象改变')
        return sorted(rows, key=lambda row: row['Id'])

    def create(self, name, payload):
        """create 只创建固定正式名称并返回ID，配置由原容器深复制而来。"""
        result = self.request('POST', self.prefix + '/containers/create?name=' + urllib.parse.quote(name), payload)
        require(isinstance(result, dict) and HEX.fullmatch(result.get('Id', '')), '新容器ID缺失')
        return result['Id']

    def stop(self, identifier):
        """stop 只停止已经绑定到本轮身份的明确容器ID。"""
        require(HEX.fullmatch(identifier), '停止ID格式异常')
        try:
            self.request('POST', self.prefix + '/containers/' + identifier + '/stop?t=20')
        except CutoverError:
            require(not self.inspect(identifier)['State']['Running'], '停止结果无法确认')

    def start(self, identifier):
        """start 只启动已核对原配置或候选配置的明确容器ID。"""
        require(HEX.fullmatch(identifier), '启动ID格式异常')
        try:
            self.request('POST', self.prefix + '/containers/' + identifier + '/start')
        except CutoverError:
            require(self.inspect(identifier)['State']['Running'], '启动结果无法确认')

    def rename(self, identifier, name):
        """rename 只按计划的精确名称保留旧对象和失败对象，不删除容器。"""
        require(HEX.fullmatch(identifier) and re.fullmatch(r'goodhr-production-(backend|frontend)-1(?:-(?:backup|failed)-clean-runtime-20261015)?', name), '改名目标不在本次范围')
        try:
            self.request('POST', self.prefix + '/containers/' + identifier + '/rename?name=' + urllib.parse.quote(name))
        except CutoverError:
            require(self.inspect(identifier)['Name'].lstrip('/') == name, '改名结果无法确认')


def validate_spec(value):
    """validate_spec 把本次迁移限制到已审阅的主机、两个原对象、两个候选镜像和精确备份名称。"""
    require(value.get('schema_version') == 1 and value.get('kind') == 'verified-runtime-repair' and value.get('host') == HOST, '计划类型或主机不匹配')
    require(value.get('release_id') == RELEASE and value.get('release_root') == str(ROOT), '发布目录或编号不匹配')
    require(COMMIT.fullmatch(value.get('source_commit', '')) is not None, '必须先填写最终ops治理提交')
    require(value.get('migration_fingerprint') == FINGERPRINT, '迁移基线不匹配')
    require(set(value.get('services', {})) == set(ROLES), '迁移服务范围必须恰好为前后端')
    for role in ROLES:
        item = value['services'][role]
        name = 'goodhr-production-' + role + '-1'
        require(item.get('name') == name and item.get('backup_name') == name + '-backup-' + RELEASE and item.get('failed_name') == name + '-failed-' + RELEASE, '正式或备份名称超出本次范围')
        require((item.get('old_container_id'), item.get('old_image')) == BASELINES[role] and item.get('new_image') == TARGETS[role], '原对象或目标镜像不匹配')
        require(item.get('app_source_commit') == ORIGINS[role], '应用来源提交不匹配')
        require(item.get('role') == role, '服务审计角色不匹配')
    return value


def validate_bindings(value):
    """validate_bindings 固定工具、登记与三份配对证明路径及SHA，禁止由计划换入任意外部脚本。"""
    names = {'guard': 'release_guard.py', 'retention': 'release_retention.py', 'policy': 'release-policy.json', 'cutover': 'release_cutover.py'}
    require(set(value.get('tools', {})) == set(names), '发布工具清单不完整')
    for key, filename in names.items():
        row = value['tools'][key]
        require(row.get('path') == str(CHECKOUT / 'goodhr5/scripts' / filename) and HEX.fullmatch(row.get('sha256', '')), '发布工具路径或SHA异常')
    expected_proofs = {str(ROOT / 'pairs' / name / 'pair-summary.json') for name in ('current', 'folders-rollback', 'tags-rollback')}
    rows = value.get('evidence_files', [])
    require(isinstance(rows, list) and len(rows) == len(expected_proofs) and {row.get('path') for row in rows} == expected_proofs and all(HEX.fullmatch(row.get('sha256', '')) for row in rows), '三份已验证配对证明不完整')
    activation = value.get('activation_evidence', {})
    require(activation.get('path') == str(ROOT / 'isolated-activation-summary.json') and HEX.fullmatch(activation.get('sha256', '')), '隔离激活证明路径或SHA异常')
    registry = value.get('registry', {})
    require(registry.get('path') in (str(ROOT / 'review-retention-registry.json'), '/opt/goodhr-release/retention-registry.json') and HEX.fullmatch(registry.get('sha256', '')), '兼容登记路径或SHA异常')
    expected_dockerfiles = {'backend': ('eca549a24be0994afaf213d1e2dfd609566f0735d46ef475ed63c7ade9353888', 'context-backend'), 'frontend': ('2b5542ad1f2df4251ce9641736c8231ddcefc664cf84e5ff59e8822c724b235f', 'context-frontend')}
    for role, (checksum, context) in expected_dockerfiles.items():
        row = value['services'][role]
        require(row.get('dockerfile') == str(ROOT / context / 'Dockerfile') and row.get('dockerfile_sha256') == checksum, '候选Dockerfile身份不匹配')


def environment(value):
    """environment 只在内存比较环境键值，并拒绝有重复键的模糊设置。"""
    rows = value or []
    require(all(isinstance(row, str) and '=' in row for row in rows), '原环境配置格式不明确')
    result = dict(row.split('=', 1) for row in rows)
    require(len(result) == len(rows), '原环境存在重复键，需人工审查')
    return result


def endpoints(value):
    """endpoints 复制网络的显式配置，忽略新容器必然变化的动态地址和端点ID。"""
    result = {}
    for name, details in value.get('NetworkSettings', {}).get('Networks', {}).items():
        entry = {key: copy.deepcopy(details[key]) for key in ('IPAMConfig', 'Links', 'Aliases', 'DriverOpts') if details.get(key)}
        if entry.get('Aliases'):
            entry['Aliases'] = [alias for alias in entry['Aliases'] if alias not in (value['Id'], value['Id'][:12])]
        result[name] = entry
    return result


def mounts(value):
    """mounts 对实际挂载源、目标和权限进行稳定比较，阻止引入匿名卷。"""
    return sorted([{key: row.get(key) for key in ('Type', 'Source', 'Destination', 'RW', 'Propagation')} for row in value.get('Mounts', [])], key=lambda row: (row['Destination'], row['Source']))


def payload_for(old, item, release_id):
    """payload_for 完整复用原Config和HostConfig，仅改变镜像与审计标签，绝不记录环境正文。"""
    require(not old['HostConfig'].get('AutoRemove') and not old['HostConfig'].get('ContainerIDFile') and not old['HostConfig'].get('VolumesFrom'), '原服务有特殊自动删除或外部ID依赖')
    require(all(row.get('Type') == 'bind' for row in old.get('Mounts', [])), '本次仅支持已经核验的绑定挂载')
    environment(old['Config'].get('Env'))
    value = copy.deepcopy(old['Config'])
    value['HostConfig'] = copy.deepcopy(old['HostConfig'])
    value['Image'] = item['new_image']
    labels = value.get('Labels') or {}
    labels.pop('goodhr.release', None)
    labels.update({'goodhr.release.id': release_id, 'goodhr.release_kind': item['role'], 'goodhr.source_commit': item['app_source_commit'], 'org.opencontainers.image.revision': item['app_source_commit'], 'goodhr.dockerfile_sha256': item['dockerfile_sha256']})
    if 'com.docker.compose.image' in labels:
        labels['com.docker.compose.image'] = item['new_image']
    value['Labels'] = labels
    if value['HostConfig'].get('NetworkMode') not in ('host', 'none'):
        value['NetworkingConfig'] = {'EndpointsConfig': endpoints(old)}
    return value


def verify_original(actual, original, item):
    """verify_original 在第一次停止之前核对原身份及全部功能配置，任何漂移都禁止切换。"""
    require(actual is not None and actual['Id'] == item['old_container_id'] and actual['Image'] == item['old_image'], '原服务身份已改变')
    require(actual['Name'].lstrip('/') == item['name'] and actual['State']['Running'], '原正式服务未正常运行')
    require(actual['Config'] == original['Config'] and actual['HostConfig'] == original['HostConfig'] and mounts(actual) == mounts(original) and endpoints(actual) == endpoints(original), '原服务功能配置发生漂移')


def verify_created(actual, old, payload, item):
    """verify_created 启动前核对daemon采用的完整功能配置，发现差异就回退而不是继续服务。"""
    require(actual is not None and actual['Image'] == item['new_image'] and not actual['State']['Running'], '新服务身份或启动状态不符')
    expected_config = {key: value for key, value in payload.items() if key not in ('HostConfig', 'NetworkingConfig')}
    for key in set(old['Config']) | set(expected_config):
        if key == 'Env':
            require(environment(actual['Config'].get(key)) == environment(old['Config'].get(key)), '新服务环境未完整保留')
        else:
            require(actual['Config'].get(key) == expected_config.get(key), '新服务配置不一致：' + key)
    require(actual['HostConfig'] == old['HostConfig'], '新服务宿主配置未完整保留')
    require(mounts(actual) == mounts(old) and endpoints(actual) == endpoints(old), '新服务挂载或网络配置未完整保留')


def safe_runtime(old):
    """safe_runtime 仅生成不含环境和日志认证选项的人工审阅摘要。"""
    config, host = old['Config'], old['HostConfig']
    return {'user': config.get('User'), 'working_dir': config.get('WorkingDir'), 'command': config.get('Cmd'), 'entrypoint': config.get('Entrypoint'), 'mounts': mounts(old), 'network_mode': host.get('NetworkMode'), 'networks': sorted(endpoints(old)), 'ports': host.get('PortBindings'), 'restart_policy': host.get('RestartPolicy'), 'memory': host.get('Memory'), 'memory_swap': host.get('MemorySwap'), 'nano_cpus': host.get('NanoCpus'), 'readonly_rootfs': host.get('ReadonlyRootfs'), 'privileged': host.get('Privileged'), 'environment_values': 'server-private-only'}


def http_probe(url, expected):
    """http_probe 只对固定公开或回环验收地址读取状态，私有请求不带凭据。"""
    try:
        with OPENER.open(url, timeout=4) as response:
            status = response.status
            raw = response.read() if url.endswith('/health') else b''
    except urllib.error.HTTPError as error:
        status, raw = error.code, b''
    require(status == expected, 'HTTP状态不符：' + urllib.parse.urlparse(url).path)
    if url.endswith('/health'):
        require(json.loads(raw).get('ok') is True, '后端健康正文未通过')


def local_health(role):
    """local_health 最多等待二十秒检查原有实际监听地址，不猜测新端口。"""
    url = 'http://127.0.0.1:8086/health' if role == 'backend' else 'http://127.0.0.1:5175/login'
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        try:
            http_probe(url, 200)
            return
        except (CutoverError, OSError, ValueError):
            time.sleep(0.5)
    raise CutoverError('正式服务本机健康检查超时：' + role)


def public_health():
    """public_health 验证两域公开页面200和未登录私有入口401，不写真实业务记录。"""
    for domain in ('goodhr.58it.cn', 'goodhr5.58it.cn'):
        for path, status in (('/', 200), ('/download', 200), ('/api/positions', 401), ('/api/auth/me', 401)):
            http_probe('https://' + domain + path, status)


def source_baseline(plan):
    """source_baseline 核对ops代码提交与干净main工作区，不执行fetch、merge、build或任何业务入口。"""
    require(socket.gethostname() == HOST and '43.228.79.108' in command(['hostname', '-I']).decode().split(), '执行主机身份不符')
    require(command(['git', '-C', str(CHECKOUT), 'rev-parse', 'HEAD']).decode().strip() == plan['source_commit'], '服务器源码未处于批准ops提交')
    require(command(['git', '-C', str(CHECKOUT), 'branch', '--show-current']).decode().strip() == 'main', '服务器源码分支不是main')
    require(not command(['git', '-C', str(CHECKOUT), 'status', '--porcelain', '--untracked-files=normal']).strip(), '服务器源码有未提交改动')


def retention_gate(plan):
    """retention_gate 调用最终只读check，要求canonical证据与实时迁移账本一致，失败不进入停止阶段。"""
    tools = plan['tools']
    for item in tools.values():
        require(digest(item['path']) == item['sha256'], '发布工具或策略摘要改变')
    for item in plan['evidence_files']:
        require(digest(item['path']) == item['sha256'], '已批准证明文件摘要改变')
    activation = plan['activation_evidence']
    require(digest(activation['path']) == activation['sha256'], '隔离激活证明在批准后改变')
    registry = plan['registry']
    require(digest(registry['path']) == registry['sha256'], '兼容版本登记在批准后改变')
    args = ['python3', tools['retention']['path'], '--policy', tools['policy']['path'], 'check', '--registry', registry['path'], '--evidence', plan['activation_evidence']['path']]
    try:
        result = json.loads(command(args, timeout=120).decode())
    except (ValueError, UnicodeError):
        raise CutoverError('retention检查结果无法确认') from None
    require(result.get('success') is True, 'retention检查未放行')
    verify_gate_result(plan, result)
    for role, item in plan['services'].items():
        require(digest(item['dockerfile']) == item['dockerfile_sha256'], '已验Dockerfile摘要改变')
        args = ['python3', tools['guard']['path'], '--policy', tools['policy']['path'], 'image', '--image', item['new_image'], '--kind', role, '--dockerfile', item['dockerfile']]
        checked = json.loads(command(args, timeout=90).decode())
        require(checked.get('success') is True and checked.get('checks', {}).get('image_id') == item['new_image'], '目标镜像来源检查未通过')
    return result


def verify_gate_result(plan, result):
    """verify_gate_result 把只读check返回值逐项绑定批准的主机、ops提交、双镜像和前后迁移指纹。"""
    expected = {'success': True, 'host': plan['host'], 'source_commit': plan['source_commit'], 'backend_image': plan['services']['backend']['new_image'], 'frontend_image': plan['services']['frontend']['new_image'], 'before_migration_fingerprint': plan['migration_fingerprint'], 'after_migration_fingerprint': plan['migration_fingerprint']}
    require(all(result.get(key) == value for key, value in expected.items()), 'retention放行结果与本次批准对象不一致')


def live_fingerprint(plan):
    """live_fingerprint 只读查询既有正式Postgres的迁移文件名，不读取密码或修改数据库。"""
    rows = command(['docker', 'exec', '-e', 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=15000 -c lock_timeout=3000', 'postgres', 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At', '-U', 'postgres', '-d', 'goodhr5', '-c', 'SELECT filename FROM schema_migrations ORDER BY filename;']).decode().splitlines()
    value = hashlib.sha256(json.dumps(sorted(rows), ensure_ascii=False, separators=(',', ':')).encode('utf-8')).hexdigest()
    require(len(rows) == 103 and value == plan['migration_fingerprint'], '实时迁移账本不再匹配已验证103库')
    return value


class Cutover:
    """以可注入引擎执行固定双服务切换，失败时仅恢复原两个容器，其他容器始终保持。"""

    def __init__(self, plan, private, engine, gate=retention_gate, source=source_baseline, ledger=live_fingerprint, health=local_health, public=public_health, save=save_json):
        """__init__ 注入只读检查和精确Docker操作，方便用假引擎验证故障回退。"""
        self.plan, self.private, self.engine = plan, private, engine
        self.gate, self.source, self.ledger, self.health, self.public, self.save = gate, source, ledger, health, public, save
        self.journal = {'schema_version': 1, 'release_id': RELEASE, 'phase': 'prepared', 'created': {}, 'events': []}

    def record(self, phase, role=None, identifier=None):
        """record 仅保存阶段、角色和对象ID，日志中不包含环境、令牌或daemon错误正文。"""
        self.journal['phase'] = phase
        self.journal['events'].append({'phase': phase, 'role': role, 'id': identifier})
        self.save(ROOT / 'cutover-journal.json', self.journal)

    def note(self, phase, role=None, identifier=None):
        """note 在故障回退中尽力记录，磁盘或日志失败不得阻断另一服务的恢复。"""
        try:
            self.record(phase, role, identifier)
        except Exception:
            self.journal['journal_write_failed'] = True

    def preserved(self):
        """preserved 核对全部非目标容器以及对象集合，发现计划外变更立即停止。"""
        originals = {value['old_container_id'] for value in self.plan['services'].values()}
        expected = {row['Id']: row for row in self.plan['containers'] if row['Id'] not in originals}
        actual = {row['Id']: row for row in self.engine.snapshot()}
        require(all(actual.get(key) == row for key, row in expected.items()), '其他容器身份、运行或重启状态改变')
        require(set(actual) == set(expected) | originals | set(self.journal['created'].values()), '出现计划外容器对象')

    def preflight(self):
        """preflight 在任何停止前核对主机、源码、批准证据、实时账本和原配置。"""
        validate_spec(self.plan)
        require(self.plan.get('simulation_only') is False, '模拟计划绝不能执行')
        self.source(self.plan)
        verify_gate_result(self.plan, self.gate(self.plan))
        self.ledger(self.plan)
        require(self.engine.snapshot() == self.plan['containers'], '批准后容器基线漂移')
        for role, item in self.plan['services'].items():
            verify_original(self.engine.inspect(item['name']), self.private['originals'][role], item)
            require(self.engine.inspect(item['backup_name'], missing=True) is None and self.engine.inspect(item['failed_name'], missing=True) is None, '精确保留名称已经存在')
            payload_for(self.private['originals'][role], item, RELEASE)
        self.preserved()

    def create(self, role):
        """create 先复制原功能配置，再登记返回ID；结果不确定时只认精确目标身份。"""
        item, old = self.plan['services'][role], self.private['originals'][role]
        payload = payload_for(old, item, RELEASE)
        try:
            identifier = self.engine.create(item['name'], payload)
        except Exception:
            uncertain = self.engine.inspect(item['name'], missing=True)
            if uncertain and uncertain['Image'] == item['new_image'] and (uncertain['Config'].get('Labels') or {}).get('goodhr.release.id') == RELEASE:
                require(uncertain['Id'] not in {row['Id'] for row in self.plan['containers']}, '未知创建结果复用了既有ID')
                self.journal['created'][role] = uncertain['Id']
                self.record('create_result_uncertain', role, uncertain['Id'])
            raise
        self.journal['created'][role] = identifier
        self.record('created', role, identifier)
        verify_created(self.engine.inspect(identifier), old, payload, item)
        self.preserved()
        self.engine.start(identifier)
        self.record('started', role, identifier)
        self.health(role)

    def restore(self):
        """restore 只在原容器仍存在且新正式身份匹配时恢复，失败对象改名保留，不删镜像或容器。"""
        for role, item in self.plan['services'].items():
            old = self.engine.inspect(item['old_container_id'], missing=True)
            require(old is not None and old['Image'] == item['old_image'] and old['Name'].lstrip('/') in (item['name'], item['backup_name']), '原容器不在已批准恢复位置，禁止猜测回退')
            before = self.private['originals'][role]
            require(old['Config'] == before['Config'] and old['HostConfig'] == before['HostConfig'] and mounts(old) == mounts(before) and endpoints(old) == endpoints(before), '原回退对象功能配置已经改变')
            current = self.engine.inspect(item['name'], missing=True)
            if current and current['Id'] != item['old_container_id']:
                require(current['Id'] == self.journal['created'].get(role) and current['Image'] == item['new_image'], '正式名称被其他容器占用，禁止停止')
                require(self.engine.inspect(item['failed_name'], missing=True) is None, '失败保留名称已经被占用')
        errors = []
        for role in ('frontend', 'backend'):
            item = self.plan['services'][role]
            current = self.engine.inspect(item['name'], missing=True)
            if current and current['Id'] != item['old_container_id']:
                try:
                    if current['State']['Running']:
                        self.engine.stop(current['Id'])
                    self.engine.rename(current['Id'], item['failed_name'])
                    self.note('failed_retained', role, current['Id'])
                except Exception:
                    errors.append(role + '_new_stop')
        for role in ROLES:
            item = self.plan['services'][role]
            try:
                old = self.engine.inspect(item['old_container_id'])
                if old['Name'].lstrip('/') == item['backup_name']:
                    self.engine.rename(old['Id'], item['name'])
                if not old['State']['Running']:
                    self.engine.start(old['Id'])
                self.note('old_restored', role, old['Id'])
                self.health(role)
            except Exception:
                errors.append(role + '_old_restore')
        for check in (self.public, lambda: self.ledger(self.plan), self.preserved):
            try:
                check()
            except Exception:
                errors.append('rollback_check')
        require(not errors, '双服务回退动作已尝试，仍有恢复或后验条件未通过')
        return {'success': True, 'old_containers_restored': True}

    def activate(self):
        """activate 持锁调用方批准后才停止原两服务，任一步失败在GC前自动恢复原对象。"""
        self.preflight()
        self.record('preflight_passed')
        try:
            for role in ('frontend', 'backend'):
                item = self.plan['services'][role]
                self.engine.stop(item['old_container_id'])
                self.record('old_stopped', role, item['old_container_id'])
                self.engine.rename(item['old_container_id'], item['backup_name'])
                self.record('old_retained', role, item['old_container_id'])
            for role in ROLES:
                self.create(role)
            self.public()
            self.ledger(self.plan)
            self.preserved()
            result = {'success': True, 'stage': 'production_activation', 'source_commit': self.plan['source_commit'], 'migration_fingerprint': FINGERPRINT, 'images': TARGETS, 'containers': dict(self.journal['created']), 'old_containers_preserved': {role: value['backup_name'] for role, value in self.plan['services'].items()}, 'other_containers_unchanged': True, 'garbage_collection_performed': False}
            self.record('activation_succeeded')
            self.save(ROOT / 'production-result.json', result)
            return result
        except Exception as error:
            self.note('activation_failed')
            restored = False
            try:
                self.restore()
                restored = True
                self.note('automatic_rollback_succeeded')
            except Exception:
                self.note('automatic_rollback_incomplete')
            try:
                self.save(ROOT / 'production-result.json', {'success': False, 'stage': 'production_activation', 'error_type': type(error).__name__, 'automatic_rollback_succeeded': restored, 'garbage_collection_performed': False})
            except Exception:
                pass
            raise CutoverError('切换未完成；' + ('原双服务已恢复' if restored else '自动回退未完整通过，请按持久journal核对')) from None


def prepare_plan(spec, engine, out):
    """prepare_plan 只读核对运行服务，私密原设置存服务器0600备份，公开计划只含非敏感审阅信息。"""
    validate_spec(spec)
    validate_bindings(spec)
    source_baseline(spec)
    retention_gate(spec)
    live_fingerprint(spec)
    snapshots, originals = engine.snapshot(), {}
    for role, item in spec['services'].items():
        old = engine.inspect(item['name'])
        require(old['Id'] == item['old_container_id'] and old['Image'] == item['old_image'] and old['State']['Running'], '原双服务基线不匹配')
        originals[role] = old
        payload_for(old, item, RELEASE)
    require(engine.snapshot() == snapshots, '只读准备期间容器发生变化')
    private_path = Path(out).with_name(Path(out).stem + '.private.json')
    require(not Path(out).exists() and not private_path.exists(), '计划或私密备份已存在，拒绝覆盖')
    save_json(private_path, {'schema_version': 1, 'originals': originals}, exclusive=True)
    plan = copy.deepcopy(spec)
    plan.update({'simulation_only': False, 'containers': snapshots, 'private_baseline': {'path': str(private_path), 'sha256': digest(private_path)}, 'runtime_review': {role: safe_runtime(old) for role, old in originals.items()}, 'prepared_at': dt.datetime.now(dt.timezone.utc).isoformat()})
    save_json(out, plan, exclusive=True)
    return {'success': True, 'plan': str(out), 'sha256': digest(out), 'services': 2, 'private_configuration_downloaded': False, 'mutation_performed': False}


def load_approved(path, expected_sha):
    """load_approved 只有精确批准的完整SHA可放行变更，模拟计划和可读私密文件一律拒绝。"""
    require(isinstance(expected_sha, str) and HEX.fullmatch(expected_sha) and digest(path) == expected_sha, '批准计划SHA不匹配')
    plan = validate_spec(read_json(path))
    validate_bindings(plan)
    require(plan.get('simulation_only') is False, '模拟计划不可执行')
    private = Path(plan['private_baseline']['path'])
    require(private.parent == ROOT and private.resolve() == private and private.is_file(), '私密配置备份路径异常')
    info = private.stat()
    require(info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and digest(private) == plan['private_baseline']['sha256'], '私密配置备份权限、归属或摘要改变')
    return plan, read_json(private)


def main(argv=None):
    """main 区分只读plan和需固定SHA的activate/rollback，不提供清理、构建或源码更新动作。"""
    parser = argparse.ArgumentParser(description='已验编译产物运行镜像迁移专用；普通新功能请使用deploy-build.sh两阶段')
    commands = parser.add_subparsers(dest='operation', required=True)
    prepare = commands.add_parser('plan')
    prepare.add_argument('--spec', required=True)
    prepare.add_argument('--out', required=True)
    for operation in ('activate', 'rollback'):
        target = commands.add_parser(operation)
        target.add_argument('--plan', required=True)
        target.add_argument('--sha256', required=True)
    args = parser.parse_args(argv)
    os.umask(0o077)
    require(Path(__file__).resolve() == CHECKOUT / 'goodhr5/scripts/release_cutover.py', '实际入口必须来自已核对的正式ops checkout')
    with deployment_locks():
        engine = DockerEngine()
        if args.operation == 'plan':
            result = prepare_plan(read_json(args.spec), engine, args.out)
        else:
            plan, private = load_approved(args.plan, args.sha256)
            runner = Cutover(plan, private, engine)
            if args.operation == 'activate':
                result = runner.activate()
            else:
                source_baseline(plan)
                live_fingerprint(plan)
                journal = ROOT / 'cutover-journal.json'
                require(journal.is_file(), '缺少本轮新对象持久记录')
                runner.journal = read_json(journal)
                require(runner.journal.get('release_id') == RELEASE, '新对象记录发布编号不匹配')
                result = runner.restore()
                save_json(ROOT / 'rollback-result.json', result)
    print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        message = str(error) if isinstance(error, CutoverError) else '任务检查或执行失败：' + type(error).__name__
        print(json.dumps({'success': False, 'error': message}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
