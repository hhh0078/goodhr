"""文件作用：仅构建、验收和切换后台统一尺寸的前端，保存完整备份且禁止数据库写入及后端重启。"""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import shutil
import signal
import socket
import subprocess
import tarfile
import urllib.request

ROOT = pathlib.Path('/opt/goodhr-release/admin-uniform-20261015')
BUILD_ROOT = pathlib.Path('/www/wwwroot/goodhr-production/releases/admin-uniform-20261015')
LOCK = pathlib.Path('/www/wwwroot/goodhr-production/backups/.goodhr-container-deploy.lock')
LAUNCHER = pathlib.Path('/opt/goodhr-release/hr-interview-20261010-63ad61cd/hr-release.py')
LAUNCHER_SHA256 = 'f23375cf68107eb2ed21637d38b2462f7346c14b82ab9a3d069893ea700fef0c'
FRONTEND = 'goodhr-production-frontend-1'
BACKEND = 'goodhr-production-backend-1'
CANDIDATE = 'goodhr-admin-uniform-frontend-candidate-20261015'
CHROME_PATH = '/downloads/goodhr-extension-chrome-1.0.9.zip'
CHROME_SHA256 = 'fefe405c45bc61d6c4b5b656554375c085c1a895c0a0c8089c509afc463107bd'
LEDGER_COUNT = 103
BASELINE = {
    BACKEND: ('f308ae790c1e6daf43e3453dd27504f7871b2cf6f1da564d95d368836646130a', 'sha256:31df9fe839f4375e0bada14816c695662029e6575d44cbac01bc2d728ce4014b', '2026-10-14T17:51:50.637741708Z'),
    FRONTEND: ('38d24aea6bc9f2542db1ef25b0de469e5e6f85310fde8d90c2ca497c498ad2ff', 'sha256:83ee8d4cbbe2a1a8ad850a8c5f165097986df63e7a23383c642e798fe6facbf0', '2026-10-14T17:51:54.11472544Z'),
    'postgres': ('7ae7305d3a222272d23cf4a68ae302cf877fc71d2ce1fab9ad3a97b3fe02d89d', 'sha256:413d57cb67bb3585acae8769a48661aa1dcac66076e3a5f88ea6c5be55f8916e', '2026-09-20T12:25:51.09539213Z'),
    'redis': ('78a704cd2eb89019c62bfb6ab2ceef603bf2cd293fe3d522ca7c132a1da72025', 'sha256:646a47c903c5c45f99081094d8c3ba51ec2a1091babf42dc5a928cef024e6fcd', '2026-09-20T12:28:59.032970342Z'),
}


def safe_existing(name):
    """仅把Docker明确的精确对象不存在视为空，权限、超时及daemon故障均停止。"""
    try:
        result = subprocess.run(['docker', 'inspect', name], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    except (subprocess.TimeoutExpired, OSError):
        raise RuntimeError('容器查询结果不明确，保留现场') from None
    if result.returncode:
        if result.stdout.strip() == b'[]' and result.stderr.strip() == ('Error: No such object: ' + name).encode():
            return None
        raise RuntimeError('容器查询失败，不能视为不存在')
    try:
        values = json.loads(result.stdout)
        if len(values) != 1 or values[0].get('Name') != '/' + name:
            raise ValueError('identity')
        return values[0]
    except (ValueError, TypeError, KeyError, IndexError):
        raise RuntimeError('容器查询对象不明确，保留现场') from None


def launcher():
    """校验固定启动器摘要后只复用安全原语，拒绝所有写SQL及非前端应用创建。"""
    if LAUNCHER.is_symlink() or hashlib.sha256(LAUNCHER.read_bytes()).hexdigest() != LAUNCHER_SHA256:
        raise RuntimeError('复用启动器摘要不一致')
    spec = importlib.util.spec_from_file_location('admin_uniform_launcher', LAUNCHER)
    hr = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(hr)
    original_sql, original_payload = hr.sql, hr.application_payload

    def readonly_sql(container, database, query, readonly=True):
        """把本次SQL调用限定为正式库只读操作，不接受写事务或其他数据库。"""
        if not readonly or container != 'postgres' or database != 'goodhr5':
            raise RuntimeError('本次发布禁止写SQL和其他数据库目标')
        return original_sql(container, database, query, readonly=True)

    def frontend_payload(root, kind, image, candidate=False):
        """正式前端完整沿用原参数，候选只绑定5182并读取现有8086公开接口。"""
        if root != ROOT or kind != 'frontend':
            raise RuntimeError('本次发布只能创建前端容器')
        payload = original_payload(root, kind, image, candidate)
        if candidate:
            env = dict(value.split('=', 1) for value in payload.get('Env') or [] if '=' in value)
            env.update({'PORT': '5182', 'HOSTNAME': '127.0.0.1', 'CLOUD_API_BASE': 'http://127.0.0.1:8086', 'NEXT_PUBLIC_CLOUD_API_BASE': 'https://goodhr.58it.cn'})
            payload['Env'] = [key + '=' + value for key, value in env.items()]
        return payload

    hr.sql = readonly_sql
    hr.existing = safe_existing
    hr.application_payload = frontend_payload
    return hr


def verify_baseline(hr, frontend=True):
    """核对固定主机和精确容器身份，切换后仍要求后端、PG及Redis启动时间不变。"""
    if socket.gethostname() != 'ldclouda31539' or '43.228.79.108' not in hr.run(['hostname', '-I']).decode().split():
        raise RuntimeError('生产服务器身份不匹配')
    result = []
    for name, expected in BASELINE.items():
        if not frontend and name == FRONTEND:
            continue
        item = hr.inspect(name)
        if (item['Id'], item['Image'], item['State']['StartedAt']) != expected or not item['State']['Running']:
            raise RuntimeError('生产基线变化：' + name)
        result.append(item)
    return result


def json_digest(value):
    """计算JSON确定性摘要，仅输出摘要而不公开配置正文。"""
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()


def extension_proof(hr):
    """只读插件完整配置行，确认Chrome1.0.9及Firefox1.0.7并保留兼容快照。"""
    row = json.loads(hr.sql('postgres', 'goodhr5', "SELECT to_jsonb(s) FROM system_configs s WHERE config_key='system.browser_extension';"))
    config = row['config_value']
    if isinstance(config, str):
        config = json.loads(config)
    if not row.get('enabled') or config.get('version_chrome') != '1.0.9' or config.get('version_firefox') != '1.0.7' or config.get('sha256_chrome') != CHROME_SHA256 or config.get('url_chrome') != 'https://goodhr.58it.cn' + CHROME_PATH or not config.get('firefox_signed') or not re.fullmatch(r'[0-9a-f]{64}', config.get('sha256_firefox') or '') or not config.get('agent_compatibility'):
        raise RuntimeError('插件配置与本次只读保留基线不一致')
    return {'row_sha256': json_digest(row), 'version_chrome': '1.0.9', 'version_firefox': '1.0.7', 'sha256_chrome': CHROME_SHA256, 'url_chrome': config.get('url_chrome'), 'sha256_firefox': config.get('sha256_firefox'), 'url_firefox': config.get('url_firefox')}


def migration_proof(hr, source_sha256):
    """只读比较归档、现有后端的103份迁移正文和正式ledger，不执行迁移。"""
    if hr.digest(ROOT / 'source.tar') != source_sha256:
        raise RuntimeError('源码归档摘要改变')
    source = hr.migrations_from_tar((ROOT / 'source.tar').read_bytes(), 'goodhr5/cloud/backend/db/migrations/')
    old = hr.image_migrations(BASELINE[BACKEND][1])
    applied = hr.ledger('postgres', 'goodhr5')
    if len(applied) != LEDGER_COUNT or applied != sorted(old) or source != old:
        raise RuntimeError('归档、现有后端103项迁移或正式ledger不一致')
    return {'ledger': applied, 'migration_sha256': {name: hashlib.sha256(text.encode()).hexdigest() for name, text in old.items()}, 'ledger_rows_hash': hr.old_ledger_snapshot('postgres', 'goodhr5', applied)}


def image_proof(hr, image_id, commit):
    """只接受完整不可变镜像ID和一致的OCI提交，不接受可变tag替代材料。"""
    if not re.fullmatch(r'sha256:[0-9a-f]{64}', image_id or ''):
        raise RuntimeError('镜像缺少完整ID')
    item = hr.inspect(image_id, image=True)
    if item['Id'] != image_id or (item['Config'].get('Labels') or {}).get('org.opencontainers.image.revision') != commit:
        raise RuntimeError('前端镜像与指定提交不一致')
    return item


def image_package_proof(hr, image):
    """用无网络校验工具读取镜像内固定Chrome包摘要，不启动应用入口。"""
    value = hr.run(['docker', 'run', '--rm', '--network=none', '--entrypoint', 'sha256sum', image, '/app/public' + CHROME_PATH]).decode().split()[0]
    if value != CHROME_SHA256:
        raise RuntimeError('镜像内Chrome安装包摘要变化')


def materials(hr, commit):
    """每阶段重查源码、完整备份、旧配置和前端镜像，防止准备后替换材料。"""
    value = hr.load(ROOT, 'manifest.private.json')
    if value['commit'] != commit or set(value['images']) != {'frontend'}:
        raise RuntimeError('提交或镜像范围不符合前端发布')
    image_proof(hr, value['images']['frontend']['id'], commit)
    if hr.digest(ROOT / 'database.dump') != value['dump']['sha256'] or hr.digest(ROOT / 'before-inspect.private.json') != value['before_inspect_sha256'] or hr.digest(ROOT / 'frontend.private.env') != value['frontend_env_sha256']:
        raise RuntimeError('备份或旧容器配置摘要变化')
    saved = hr.load(ROOT, 'before-inspect.private.json')
    expected = {item['Name'].lstrip('/'): (item['Id'], item['Image'], item['State']['StartedAt']) for item in saved}
    if expected != BASELINE or migration_proof(hr, value['source_sha256']) != value['migration'] or extension_proof(hr) != value['extension']:
        raise RuntimeError('原运行基线、迁移或插件只读配置变化')
    image_package_proof(hr, value['images']['frontend']['id'])
    hr.application_payload(ROOT, 'frontend', value['images']['frontend']['id'])
    return value


def build(hr, args):
    """核对Git归档并在数据盘构建前端，使用国内镜像和资源上限，不操作正式容器。"""
    verify_baseline(hr)
    archive = pathlib.Path(args.archive or '')
    if not archive.is_file() or archive.is_symlink() or not re.fullmatch(r'[0-9a-f]{64}', args.archive_sha256 or '') or hr.digest(archive) != args.archive_sha256:
        raise RuntimeError('上传归档或摘要不一致')
    if archive.stat().st_size > 300 * 1024 ** 2 or shutil.disk_usage(BUILD_ROOT).free < 3 * 1024 ** 3 or shutil.disk_usage(ROOT).free < archive.stat().st_size + 256 * 1024 ** 2:
        raise RuntimeError('归档异常或发布磁盘余量不足')
    if any(path.exists() for path in (ROOT / 'source.tar', ROOT / 'build.private.json', ROOT / 'build.private.log', BUILD_ROOT / 'source', BUILD_ROOT / 'frontend-runtime')):
        raise RuntimeError('已有构建现场，禁止覆盖')
    with tarfile.open(archive) as stream:
        if stream.pax_headers.get('comment') != args.commit:
            raise RuntimeError('Git归档提交不匹配')
        names = set()
        for member in stream.getmembers():
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or member.name in names or not (member.isfile() or member.isdir()):
                raise RuntimeError('源码归档路径、重复条目或链接不安全')
            names.add(member.name)
        source = BUILD_ROOT / 'source'
        source.mkdir(mode=0o700)
        stream.extractall(source)
    shutil.copyfile(archive, ROOT / 'source.tar')
    frontend = source / 'goodhr5/cloud/frontend-next'
    if not (frontend / 'package-lock.json').is_file() or hr.digest(frontend / ('public' + CHROME_PATH)) != CHROME_SHA256:
        raise RuntimeError('前端锁文件缺失或Chrome安装包改变')
    migration_proof(hr, args.archive_sha256)
    tag = 'goodhr-production-frontend:admin-uniform-' + args.commit[:8]
    builder = 'goodhr-admin-uniform-build-' + args.commit[:12]
    if hr.existing(builder):
        raise RuntimeError('构建容器名称已占用')
    with (ROOT / 'build.private.log').open('xb') as log:
        command = 'npm ci --include=dev --registry=https://registry.npmmirror.com --no-audit --no-fund && npm run build'
        hr.run(['docker', 'run', '--rm', '--memory=2048m', '--memory-swap=3072m', '--cpus=2', '--name', builder, '--label', 'goodhr.release=' + ROOT.name, '--label', 'goodhr.source_commit=' + args.commit, '--label', 'goodhr.release_kind=build', '--entrypoint', 'sh', '--mount', 'type=bind,src=' + str(frontend) + ',dst=/work', '-w', '/work', '-e', 'NODE_ENV=production', '-e', 'NODE_OPTIONS=--max-old-space-size=1536', '-e', 'CIRCLE_NODE_TOTAL=2', '-e', 'NEXT_TELEMETRY_DISABLED=1', '-e', 'NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'NEXT_PUBLIC_SITE_URL=https://goodhr.58it.cn', BASELINE[FRONTEND][1], '-c', command], output=log, timeout=1800)
        runtime = BUILD_ROOT / 'frontend-runtime'
        runtime.mkdir(mode=0o700)
        shutil.copytree(frontend / '.next/standalone', runtime / 'standalone')
        shutil.copytree(frontend / '.next/static', runtime / 'static')
        shutil.copytree(frontend / 'public', runtime / 'public')
        dockerfile = ['FROM ' + BASELINE[FRONTEND][1], 'LABEL org.opencontainers.image.revision=' + args.commit, 'COPY standalone /app/', 'COPY static /app/.next/static', 'COPY public /app/public']
        (runtime / 'Dockerfile').write_text('\n'.join(dockerfile) + '\n')
        hr.run(['docker', 'build', '--network=none', '-t', tag, str(runtime)], output=log, timeout=900)
    image = hr.inspect(tag, image=True)['Id']
    image_proof(hr, image, args.commit)
    image_package_proof(hr, image)
    verify_baseline(hr)
    proof = {'commit': args.commit, 'source_sha256': args.archive_sha256, 'images': {'frontend': {'id': image, 'tag': tag}}}
    hr.save(ROOT, 'build.private.json', proof)
    print(json.dumps({'build': 'complete', **proof}))


def prepare(hr, args):
    """保存完整旧配置与Env，制作只读PG完整备份并校验目录，不恢复库或执行迁移。"""
    current = verify_baseline(hr)
    if any((ROOT / name).exists() for name in ('manifest.private.json', 'before-inspect.private.json', 'frontend.private.env', 'database.dump', 'database.dump.partial')):
        raise RuntimeError('已有准备材料，禁止覆盖')
    proof = hr.load(ROOT, 'build.private.json')
    if proof['commit'] != args.commit or set(proof['images']) != {'frontend'}:
        raise RuntimeError('缺少本轮仅前端构建证明')
    image_proof(hr, proof['images']['frontend']['id'], args.commit)
    migration = migration_proof(hr, proof['source_sha256'])
    extension = extension_proof(hr)
    for image in (BASELINE[FRONTEND][1], proof['images']['frontend']['id']):
        image_package_proof(hr, image)
    if shutil.disk_usage(ROOT).free < 256 * 1024 ** 2:
        raise RuntimeError('根盘完整备份余量不足')
    size = int(hr.sql('postgres', 'goodhr5', 'SELECT pg_database_size(current_database());'))
    hr.save(ROOT, 'before-inspect.private.json', current)
    old = next(item for item in current if item['Name'] == '/' + FRONTEND)
    with (ROOT / 'frontend.private.env').open('x', encoding='utf-8') as output:
        output.write('\n'.join(old['Config'].get('Env') or []) + '\n')
        output.flush()
        os.fsync(output.fileno())
    dump = ROOT / 'database.dump.partial'
    with dump.open('xb') as output:
        hr.run(['docker', 'exec', 'postgres', 'sh', '-c', 'exec pg_dump -U "$POSTGRES_USER" -d goodhr5 -Fc --no-owner --no-acl'], output=output, timeout=900)
        output.flush()
        os.fsync(output.fileno())
    with dump.open('rb') as stream:
        if stream.read(5) != b'PGDMP':
            raise RuntimeError('完整备份不是PGDMP格式')
    with dump.open('rb') as stream:
        catalog = hr.run(['docker', 'exec', '-i', 'postgres', 'pg_restore', '--list'], input_file=stream)
    if not catalog:
        raise RuntimeError('完整备份目录校验失败')
    dump.replace(ROOT / 'database.dump')
    hr.save(ROOT, 'dump-catalog.private.json', {'bytes': len(catalog), 'sha256': hashlib.sha256(catalog).hexdigest()})
    manifest = {**proof, 'migration': migration, 'extension': extension, 'database_bytes': size, 'before_inspect_sha256': hr.digest(ROOT / 'before-inspect.private.json'), 'frontend_env_sha256': hr.digest(ROOT / 'frontend.private.env'), 'dump': {'bytes': (ROOT / 'database.dump').stat().st_size, 'sha256': hr.digest(ROOT / 'database.dump')}}
    hr.save(ROOT, 'manifest.private.json', manifest)
    materials(hr, args.commit)
    verify_baseline(hr)
    print(json.dumps({'prepare': 'complete', 'commit': args.commit, 'ledger_count': LEDGER_COUNT, 'sql_writes': 0, 'dump': manifest['dump']}))


def public_release(base, browser):
    """匿名读取公开插件版本，不发送登录信息或修改发布配置。"""
    with urllib.request.urlopen(base + '/api/system/browser-extension?browser=' + browser, timeout=20) as response:
        return json.load(response)['release']


def verify_http(hr, frontend, public=False):
    """只发送匿名GET检查页面、权限边界、公开版本和固定Chrome包摘要。"""
    backend = 'https://goodhr.58it.cn' if public else 'http://127.0.0.1:8086'
    hr.wait_http('http://127.0.0.1:8086/health')
    for path in ('/api/admin/users', '/api/candidates', '/api/candidates/folders', '/api/candidate-recommendations', '/api/recruitment/company'):
        hr.wait_http(backend + path, 401)
    for path in ('/', '/login', '/download', '/admin', '/admin/positions', '/admin/resumes', '/admin/recommendations', '/admin/recruitment', '/admin/recruitment/settings', '/admin/personal-config', '/admin/users', '/admin/logs', '/admin/system-config', '/admin/accounts', '/admin/team', '/admin/team-stats', '/admin/payment-records', '/admin/ai-records', '/admin/activation-codes', '/admin/mail', '/admin/agent-download', '/admin/help', '/admin/local-data', '/admin/invitations', '/admin/subscription'):
        hr.wait_http(frontend + path)
    chrome, firefox = public_release(backend, 'chrome'), public_release(backend, 'firefox')
    expected = hr.load(ROOT, 'manifest.private.json')['extension']
    if chrome.get('version') != '1.0.9' or chrome.get('sha256_chrome') != CHROME_SHA256 or chrome.get('url_chrome') != expected['url_chrome'] or firefox.get('version') != '1.0.7' or not firefox.get('firefox_signed') or firefox.get('sha256_firefox') != expected['sha256_firefox'] or firefox.get('url_firefox') != expected['url_firefox']:
        raise RuntimeError('公开插件版本、签名或摘要变化')
    digest = hashlib.sha256()
    with urllib.request.urlopen(frontend + CHROME_PATH, timeout=20) as response:
        for block in iter(lambda: response.read(1024 * 1024), b''):
            digest.update(block)
    if digest.hexdigest() != CHROME_SHA256:
        raise RuntimeError('公开Chrome包摘要变化')


def recognize(manifest, item, identity=None):
    """按镜像、提交、发布和类型标签识别本轮前端，创建超时也不会操作外来容器。"""
    labels = item['Config'].get('Labels') or {}
    if item['Image'] != manifest['images']['frontend']['id'] or (identity and item['Id'] != identity) or labels.get('goodhr.release') != ROOT.name or labels.get('goodhr.source_commit') != manifest['commit'] or labels.get('goodhr.release_kind') != 'frontend':
        raise RuntimeError('容器不属于本次前端发布，保留现场')


def rehearse(hr, args):
    """仅启动5182候选前端并匿名验收，沿用现有后端只读公开接口，不建立恢复库。"""
    verify_baseline(hr)
    manifest = materials(hr, args.commit)
    if any((ROOT / name).exists() for name in ('rehearsal.private.json', 'rehearsal-result.private.json')) or hr.existing(CANDIDATE):
        raise RuntimeError('已有候选验收现场，禁止覆盖')
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 5182))
    state = {'name': CANDIDATE, 'id': None, 'image': manifest['images']['frontend']['id'], 'commit': args.commit, 'cleaned': False}
    hr.save(ROOT, 'rehearsal.private.json', state)
    identity = hr.create_application(ROOT, 'frontend', CANDIDATE, state['image'], candidate=True)
    state['id'] = identity
    hr.save(ROOT, 'rehearsal.private.json', state)
    hr.start_container(identity)
    verify_http(hr, 'http://127.0.0.1:5182')
    recognize(manifest, hr.inspect(CANDIDATE), identity)
    verify_baseline(hr)
    materials(hr, args.commit)
    result = {'ready': True, 'commit': args.commit, 'images': manifest['images'], 'candidate_id': identity, 'ledger_count': LEDGER_COUNT, 'backend_unchanged': True, 'sql_writes': 0}
    hr.save(ROOT, 'rehearsal-result.private.json', result)
    print(json.dumps({'rehearse': 'complete', **result}))


def rollback(hr, args):
    """仅恢复精确旧前端，保留数据库、后端和失败现场，不恢复dump或删除镜像。"""
    verify_baseline(hr, frontend=False)
    manifest = hr.load(ROOT, 'manifest.private.json')
    state = hr.load(ROOT, 'cutover-progress.private.json')
    if manifest['commit'] != args.commit or state.get('changed') not in ([], ['frontend']):
        raise RuntimeError('回退材料不属于本次仅前端操作')
    if state['changed']:
        current = hr.existing(FRONTEND)
        if current and current['Id'] != BASELINE[FRONTEND][0]:
            recognize(manifest, current, state['new'].get('frontend'))
            state['new']['frontend'] = current['Id']
            failed = FRONTEND + '-failed-' + ROOT.name + '-' + current['Id'][:12]
            if hr.existing(failed):
                raise RuntimeError('失败前端保留名已占用')
            hr.intent(ROOT, state, 'rollback_stop_new_frontend')
            if current['State']['Running']:
                hr.run(['docker', 'stop', '-t', '20', current['Id']], timeout=45)
            hr.intent(ROOT, state, 'rollback_preserve_failed_frontend')
            hr.run(['docker', 'rename', current['Id'], failed])
            current = None
        if current is None:
            saved = hr.existing(FRONTEND + '-backup-' + ROOT.name)
            if not saved or (saved['Id'], saved['Image']) != BASELINE[FRONTEND][:2]:
                raise RuntimeError('旧前端备份身份不匹配')
            hr.intent(ROOT, state, 'rollback_restore_old_frontend')
            hr.run(['docker', 'rename', saved['Id'], FRONTEND])
            current = hr.inspect(FRONTEND)
        if (current['Id'], current['Image']) != BASELINE[FRONTEND][:2]:
            raise RuntimeError('恢复目标不是精确旧前端')
        if not current['State']['Running']:
            hr.intent(ROOT, state, 'rollback_start_old_frontend')
            hr.start_container(current['Id'])
        hr.wait_http('http://127.0.0.1:5175/login')
    verify_baseline(hr, frontend=False)
    state['rolled_back'] = True
    hr.intent(ROOT, state, 'rollback_complete')
    result = {'restored': True, 'commit': args.commit, 'backend_unchanged': True, 'database_restored': False, 'sql_writes': 0, 'extension_unchanged': extension_proof(hr) == manifest['extension']}
    hr.save(ROOT, 'rollback-result.private.json', result)
    print(json.dumps({'rollback': 'complete', **result}))
    if not result['extension_unchanged']:
        raise RuntimeError('旧前端已恢复，但插件配置有外部变化，需核对')


def cutover(hr, args):
    """验收后只停旧前端并切换新前端，异常和SIGTERM按精确身份自动恢复。"""
    verify_baseline(hr)
    manifest = materials(hr, args.commit)
    ready = hr.load(ROOT, 'rehearsal-result.private.json')
    if not ready.get('ready') or ready['commit'] != args.commit or ready['images'] != manifest['images'] or (ROOT / 'cutover-progress.private.json').exists() or hr.existing(FRONTEND + '-backup-' + ROOT.name):
        raise RuntimeError('缺少本轮验收证明或已有切换现场')
    state = {'changed': [], 'new': {}, 'rolled_back': False, 'commit': args.commit}
    hr.intent(ROOT, state, 'begin_cutover')
    try:
        verify_baseline(hr)
        saved = next(item for item in hr.load(ROOT, 'before-inspect.private.json') if item['Name'] == '/' + FRONTEND)
        observed = hr.inspect(FRONTEND)
        if any(observed.get(key) != saved.get(key) for key in ('Config', 'HostConfig', 'Mounts')) or observed.get('NetworkSettings', {}).get('Networks') != saved.get('NetworkSettings', {}).get('Networks'):
            raise RuntimeError('旧前端运行配置在准备后发生变化')
        state['changed'].append('frontend')
        hr.intent(ROOT, state, 'stop_old_frontend')
        hr.run(['docker', 'stop', '-t', '20', BASELINE[FRONTEND][0]], timeout=45)
        hr.intent(ROOT, state, 'preserve_old_frontend')
        hr.run(['docker', 'rename', BASELINE[FRONTEND][0], FRONTEND + '-backup-' + ROOT.name])
        hr.intent(ROOT, state, 'create_new_frontend')
        identity = hr.create_application(ROOT, 'frontend', FRONTEND, manifest['images']['frontend']['id'])
        state['new']['frontend'] = identity
        hr.intent(ROOT, state, 'start_new_frontend')
        hr.start_container(identity)
        verify_http(hr, 'http://127.0.0.1:5175')
        verify_http(hr, 'https://goodhr.58it.cn', public=True)
        recognize(manifest, hr.inspect(FRONTEND), identity)
        verify_baseline(hr, frontend=False)
        materials(hr, args.commit)
        state['complete'] = True
        hr.intent(ROOT, state, 'cutover_complete')
        result = {'complete': True, 'commit': args.commit, 'new_frontend_id': identity, 'images': manifest['images'], 'backend_unchanged': True, 'pg_redis_unchanged': True, 'ledger_count': LEDGER_COUNT, 'sql_writes': 0, 'extension_unchanged': True}
        hr.save(ROOT, 'deployment-result.private.json', result)
        print(json.dumps({'cutover': 'complete', **result}))
    except BaseException:
        previous_term = signal.signal(signal.SIGTERM, signal.SIG_IGN)
        previous_int = signal.signal(signal.SIGINT, signal.SIG_IGN)
        try:
            rollback(hr, args)
        except BaseException:
            raise RuntimeError('前端发布未完成且自动恢复受阻，请核对私有进度；后端、PG和Redis未操作') from None
        finally:
            signal.signal(signal.SIGTERM, previous_term)
            signal.signal(signal.SIGINT, previous_int)
        raise RuntimeError('前端发布未完成，精确旧前端已恢复') from None


def cleanup(hr, args):
    """仅清理精确本轮5182候选，保留正式及备份容器、完整dump、源码和全部镜像。"""
    verify_baseline(hr, frontend=False)
    manifest = hr.load(ROOT, 'manifest.private.json')
    state = hr.load(ROOT, 'rehearsal.private.json')
    if manifest['commit'] != args.commit or state.get('commit') != args.commit or state.get('name') != CANDIDATE or state.get('image') != manifest['images']['frontend']['id']:
        raise RuntimeError('候选清理材料不符合本轮约束')
    current = hr.existing(CANDIDATE)
    if current:
        recognize(manifest, current, state.get('id'))
        if current['HostConfig'].get('NetworkMode') != 'host' or 'PORT=5182' not in (current['Config'].get('Env') or []) or 'HOSTNAME=127.0.0.1' not in (current['Config'].get('Env') or []):
            raise RuntimeError('候选网络或端口约束变化')
        state['id'] = current['Id']
        state['last_operation'] = 'cleanup_candidate'
        hr.save(ROOT, 'rehearsal.private.json', state)
        if current['State']['Running']:
            hr.run(['docker', 'stop', '-t', '10', current['Id']], timeout=30)
        hr.run(['docker', 'rm', current['Id']])
    state['cleaned'] = True
    hr.save(ROOT, 'rehearsal.private.json', state)
    verify_baseline(hr, frontend=False)
    result = {'removed_candidate': CANDIDATE, 'backup_dump_images_retained': True, 'backend_unchanged': True}
    hr.save(ROOT, 'cleanup-result.private.json', result)
    print(json.dumps({'cleanup': 'complete', **result}))


def main():
    """固定发布目录与跨发布锁，按明确提交执行单阶段操作，所有输出仅含白名单摘要。"""
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=('build', 'prepare', 'rehearse', 'cutover', 'rollback', 'cleanup'))
    parser.add_argument('--commit', required=True)
    parser.add_argument('--archive')
    parser.add_argument('--archive-sha256')
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9a-f]{40}', args.commit or ''):
        raise RuntimeError('必须指定完整Git提交')
    os.umask(0o077)
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    BUILD_ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    if ROOT.is_symlink() or ROOT.resolve() != ROOT or ROOT.stat().st_dev != pathlib.Path('/').stat().st_dev or BUILD_ROOT.is_symlink() or BUILD_ROOT.resolve() != BUILD_ROOT or BUILD_ROOT.stat().st_dev != pathlib.Path('/www').stat().st_dev:
        raise RuntimeError('发布根盘或构建数据盘目录不匹配')
    ROOT.chmod(0o700)
    BUILD_ROOT.chmod(0o700)
    with LOCK.open('a') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        hr = launcher()
        signal.signal(signal.SIGTERM, hr.interrupt_release)
        operations = {'build': build, 'prepare': prepare, 'rehearse': rehearse, 'cutover': cutover, 'rollback': rollback, 'cleanup': cleanup}
        operations[args.operation](hr, args)


if __name__ == '__main__':
    try:
        main()
    except (Exception, KeyboardInterrupt):
        print(json.dumps({'status': 'failed', 'message': '发布阶段未完成，请核对本次私有进度及精确容器状态；没有执行SQL写入。'}, ensure_ascii=False))
        raise SystemExit(1)
