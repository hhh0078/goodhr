"""文件作用：在独立发布目录用已缓存依赖构建用户金额统计和简历UI镜像，不安装依赖、不操作正式容器或数据库。"""
import hashlib
import json
import os
import pathlib
import shutil
import socket
import subprocess
import tarfile
import time

ROOT = pathlib.Path('/opt/goodhr-release/admin-ai-finance-ui-20261011')
COMMIT = '2f70a144fdc250be524cfa4e19dd07d70bf697f4'
SOURCE = pathlib.Path('/www/server/panel/data/agent/mcp_uploads/files/key_da180f35c962/EEG41vhBVYp2ZdEs/admin-ai-finance-source-2f70a144.tar')
BINARY = pathlib.Path('/www/server/panel/data/agent/mcp_uploads/files/key_da180f35c962/NNpcvjAzlQG8nJh1/goodhr-server-2f70a144')
SOURCE_HASH = '45360e39ab8524ba4e74d5c9730467a9087b8b90b7d2e4310f4d9b90a90e81a3'
BINARY_HASH = 'c41f14adc43658ab26514fff634534a07afe1130448fd6a02beedab4483d69db'
NODE_IMAGE = 'sha256:7c3b093add7c43400ee83b815ab2cda98794a10045bcf76ce9bb2f89b97cbc5c'
DEPS_IMAGE = 'sha256:01bd78d7d6aea338560b3203070ab0f8c7117835533b57d58f563e59816e5358'
BACKEND_IMAGE = 'sha256:f82fa9573811363b3bbeeab428e776371f3830ed9fa32bcd43815b3044750477'


def digest(path):
    """计算不可变输入与发布产物摘要，不读取任何账号配置。"""
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def run(args, log=None):
    """执行有限且明确的构建操作，失败时保留日志而不公开凭据。"""
    if log:
        with (ROOT / log).open('xb') as output:
            result = subprocess.run(args, stdout=output, stderr=subprocess.STDOUT, timeout=900)
    else:
        result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=900)
    if result.returncode:
        raise RuntimeError('构建操作未完成，请核对独立目录日志')
    return result.stdout


def main():
    """核对源提交、归档和缓存依赖后构建两个镜像，仅保存构建证明。"""
    os.umask(0o077)
    started = time.monotonic()
    if socket.gethostname() != 'ldclouda31539' or ROOT.exists():
        raise RuntimeError('服务器不符或本轮目录已存在，禁止覆盖')
    if digest(SOURCE) != SOURCE_HASH or digest(BINARY) != BINARY_HASH:
        raise RuntimeError('上传输入摘要不一致')
    ROOT.mkdir(mode=0o700)
    shutil.copyfile(SOURCE, ROOT / 'source.tar')
    shutil.copyfile(BINARY, ROOT / 'goodhr-server')
    (ROOT / 'goodhr-server').chmod(0o755)
    source_root = ROOT / 'source'
    source_root.mkdir(mode=0o700)
    with tarfile.open(ROOT / 'source.tar') as archive:
        for member in archive.getmembers():
            name = pathlib.PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts or not (member.isdir() or member.isfile()):
                raise RuntimeError('源码归档包含越界路径或链接')
            if not member.name.startswith(('goodhr5/cloud/backend', 'goodhr5/cloud/frontend-next', 'goodhr5/cloud', 'goodhr5')):
                raise RuntimeError('源码归档超出本轮应用范围')
        archive.extractall(source_root)
    frontend = source_root / 'goodhr5/cloud/frontend-next'
    deps_name = 'goodhr-finance-deps-2f70a144'
    run(['docker', 'create', '--name', deps_name, DEPS_IMAGE])
    try:
        run(['docker', 'cp', deps_name + ':/app/package-lock.json', str(ROOT / 'cached-package-lock.json')])
        if digest(ROOT / 'cached-package-lock.json') != digest(frontend / 'package-lock.json'):
            raise RuntimeError('缓存依赖与本轮lock不同，禁止继续')
        run(['docker', 'cp', deps_name + ':/app/node_modules', str(frontend / 'node_modules')])
    finally:
        run(['docker', 'rm', deps_name])
    run(['docker', 'run', '--rm', '--name', 'goodhr-finance-build-2f70a144', '--memory', '2g', '--memory-swap', '3g', '--cpus', '1',
         '-v', str(frontend) + ':/app', '-w', '/app', '-e', 'NODE_OPTIONS=--max-old-space-size=1536', '-e', 'CI=1', '-e', 'CIRCLE_NODE_TOTAL=1',
         '-e', 'NEXT_TELEMETRY_DISABLED=1', '-e', 'CLOUD_API_BASE=https://goodhr.58it.cn', '-e', 'NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr.58it.cn',
         '-e', 'NEXT_PUBLIC_SITE_URL=https://goodhr.58it.cn', '-e', 'NEXT_PUBLIC_LOCAL_AGENT_PORT=', NODE_IMAGE,
         'node', 'node_modules/next/dist/bin/next', 'build', '--webpack'], 'frontend-build.log')
    runtime = ROOT / 'frontend-runtime'
    shutil.copytree(frontend / '.next/standalone', runtime)
    shutil.copytree(frontend / '.next/static', runtime / '.next/static')
    shutil.copytree(frontend / 'public', runtime / 'public')
    (runtime / 'Dockerfile').write_text('FROM ' + NODE_IMAGE + '\nENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0\nLABEL org.opencontainers.image.revision="' + COMMIT + '"\nWORKDIR /app\nCOPY . /app\nEXPOSE 3000\nCMD ["node", "server.js"]\n')
    backend = ROOT / 'backend-runtime'
    backend.mkdir(mode=0o700)
    shutil.copyfile(ROOT / 'goodhr-server', backend / 'goodhr-server')
    (backend / 'goodhr-server').chmod(0o755)
    shutil.copytree(source_root / 'goodhr5/cloud/backend/db/migrations', backend / 'db/migrations')
    (backend / 'Dockerfile').write_text('FROM ' + BACKEND_IMAGE + '\nLABEL org.opencontainers.image.revision="' + COMMIT + '"\nCOPY goodhr-server /usr/local/bin/goodhr-server\nCOPY db/migrations /app/db/migrations\n')
    images = {}
    for kind, context in [('backend', backend), ('frontend', runtime)]:
        tag = 'goodhr-production-' + kind + ':admin-ai-finance-ui-20261011-2f70a144'
        run(['docker', 'build', '--network=none', '-t', tag, str(context)], kind + '-image.log')
        info = json.loads(run(['docker', 'image', 'inspect', tag]))[0]
        if info['Config'].get('Labels', {}).get('org.opencontainers.image.revision') != COMMIT:
            raise RuntimeError('构建镜像提交标签不一致')
        images[kind] = {'name': tag, 'id': info['Id'], 'bytes': info['Size']}
    result = {'commit': COMMIT, 'images': images, 'source_sha256': SOURCE_HASH, 'binary_sha256': BINARY_HASH,
              'dependency_lock_sha256': digest(frontend / 'package-lock.json'), 'build_seconds': round(time.monotonic() - started, 3)}
    (ROOT / 'build-result.json').write_text(json.dumps(result, indent=2))
    print(json.dumps(result))


if __name__ == '__main__':
    main()
