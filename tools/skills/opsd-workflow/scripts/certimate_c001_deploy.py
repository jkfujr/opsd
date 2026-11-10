#!/usr/bin/python3
import fcntl
import json
import os
import pathlib
import socket
import ssl
import stat
import subprocess
import tempfile
import time

INCOMING = pathlib.Path('/var/lib/certimate-deploy/incoming')
DESTINATIONS = [pathlib.Path('/1panel/1panel/apps/openresty/openresty/www/sites/mail/ssl'), pathlib.Path('/home/Server/PMail/config/ssl')]
OPENRESTY = '1Panel-openresty-O6IV'

def run(args, **options):
    return subprocess.run(args, check=True, capture_output=True, timeout=45, **options).stdout

def incoming(name):
    fd = os.open(INCOMING / name, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as file:
        info = os.fstat(file.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size < 1024 * 1024:
            raise RuntimeError('证书交付文件无效')
        return file.read()

def replace(path, body, previous):
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix='.certimate-', delete=False) as file:
        temp = pathlib.Path(file.name)
        os.fchmod(file.fileno(), stat.S_IMODE(previous.st_mode))
        os.fchown(file.fileno(), previous.st_uid, previous.st_gid)
        file.write(body)
        file.flush()
        os.fsync(file.fileno())
    os.replace(temp, path)

def served(port):
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    with socket.create_connection(('127.0.0.1', port), timeout=5) as connection:
        with context.wrap_socket(connection, server_hostname='mail.kagura-mea.com') as tls:
            return tls.getpeercert(binary_form=True)

def wait_certificate(port, expected):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        try:
            if served(port) == expected:
                return
        except (OSError, ssl.SSLError):
            pass
        time.sleep(0.5)
    raise RuntimeError('服务尚未加载交付证书：%d' % port)

def deploy():
    cert, key = incoming('fullchain.pem'), incoming('privkey.pem')
    with tempfile.TemporaryDirectory(prefix='certimate-c001-', dir='/run') as directory:
        cert_path, key_path = pathlib.Path(directory, 'fullchain.pem'), pathlib.Path(directory, 'privkey.pem')
        cert_path.write_bytes(cert)
        key_path.write_bytes(key)
        public = run(['openssl', 'x509', '-in', str(cert_path), '-pubkey', '-noout'])
        if public != run(['openssl', 'pkey', '-in', str(key_path), '-pubout']):
            raise RuntimeError('证书与私钥不匹配')
        run(['openssl', 'x509', '-in', str(cert_path), '-checkend', '86400', '-noout'])
        names = run(['openssl', 'x509', '-in', str(cert_path), '-noout', '-ext', 'subjectAltName']).decode()
        domains = {part.strip()[4:] for part in names.splitlines()[-1].split(',') if part.strip().startswith('DNS:')}
        if not {'kagura-mea.com', '*.kagura-mea.com'}.issubset(domains):
            raise RuntimeError('交付证书缺少指定域名')
        expires = run(['openssl', 'x509', '-in', str(cert_path), '-enddate', '-noout']).decode().strip()
    expected = ssl.PEM_cert_to_DER_cert(cert.decode().split('-----END CERTIFICATE-----', 1)[0] + '-----END CERTIFICATE-----\n')
    old = []
    for directory in DESTINATIONS:
        if not directory.is_dir() or directory.is_symlink():
            raise RuntimeError('原证书目录无效')
        for name, body in [('fullchain.pem', cert), ('privkey.pem', key)]:
            path = directory / name
            info = path.lstat()
            if not stat.S_ISREG(info.st_mode):
                raise RuntimeError('原证书文件不是普通文件')
            previous = path.read_bytes()
            if previous != body:
                old.append((path, previous, info))
                replace(path, body, info)
    try:
        run(['docker', 'exec', OPENRESTY, 'nginx', '-t'])
    except subprocess.CalledProcessError:
        for path, body, info in old:
            replace(path, body, info)
        raise
    # 先读取实际加载结果，同一证书不会再次重启服务。
    reloaded = []
    if served(443) != expected:
        run(['docker', 'exec', OPENRESTY, 'nginx', '-s', 'reload'])
        wait_certificate(443, expected)
        reloaded.append('OpenResty')
    if served(465) != expected:
        run(['docker', 'restart', '--time', '30', 'Pmail'])
        wait_certificate(465, expected)
        reloaded.append('PMail')
    print(json.dumps({'deployed': True, 'expires': expires, 'changed_files': len(old), 'reloaded': reloaded}, ensure_ascii=False))

if __name__ == '__main__':
    with open('/run/lock/certimate-c001.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        deploy()
