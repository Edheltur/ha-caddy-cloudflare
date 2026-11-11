#!/usr/bin/env python3
"""Opt-in real DNS-01 test. Requires a disposable zone-scoped CF_API_TOKEN.
Never run in pull_request workflows. Logs remain private; only generic errors print.
"""
import hashlib,json,os,socket,ssl,subprocess,sys,tempfile,time
from pathlib import Path
from defaults import load_default_config
ROOT=Path(__file__).resolve().parents[1]
BIN=str(Path(sys.argv[1] if len(sys.argv)>1 else ROOT/'caddy-bin').resolve())
token=os.environ['CF_API_TOKEN'];domain=os.environ['ACME_TEST_DOMAIN']
assert domain and '*' not in domain and '/' not in domain
assert os.environ.get('ACME_ALLOW_DNS_WRITE')=='yes', 'explicit opt-in required'
port=20443
with tempfile.TemporaryDirectory(prefix='acme-staging-') as td:
    root=Path(td);state=root/'state';state.mkdir(mode=0o700)
    c=load_default_config()
    c['storage']['root']=str(state)
    srv=c['apps']['http']['servers']['proxy'];srv['listen']=['127.0.0.1:'+str(port)]
    srv['tls_connection_policies']=[{'match':{'sni':[domain]},'protocol_min':'tls1.2'}]
    srv['routes']=[{'match':[{'host':[domain]}],'handle':[{'handler':'static_response','body':'staging-ok'}]}]
    tls=c['apps']['tls'];tls['certificates']['automate']=[domain]
    policy=tls['automation']['policies'][0];policy['subjects']=[domain]
    policy['issuers'][0]['ca']='https://acme-staging-v02.api.letsencrypt.org/directory'
    (root/'caddy.json').write_text(json.dumps(c))
    previous=None
    for n in range(2):
        with open(root/f'run{n}.log','w+') as log:
            p=subprocess.Popen([BIN,'run','--config',str(root/'caddy.json')],stdout=log,stderr=log)
            try:
                deadline=time.monotonic()+300
                while True:
                    try:
                        ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT);ctx.check_hostname=False;ctx.verify_mode=ssl.CERT_NONE # staging CA is not publicly trusted
                        with ctx.wrap_socket(socket.create_connection(('127.0.0.1',port),timeout=3),server_hostname=domain) as s:
                            s.sendall(f'GET / HTTP/1.1\r\nHost: {domain}\r\nConnection: close\r\n\r\n'.encode())
                            assert b'200' in s.recv(4096).split(b'\r\n')[0]
                        break
                    except (OSError,AssertionError):
                        if p.poll() is not None or time.monotonic()>deadline:raise RuntimeError('staging issuance failed; inspect private local logs without publishing them')
                        time.sleep(2)
                files={str(f.relative_to(state)):hashlib.sha256(f.read_bytes()).hexdigest() for f in state.rglob('*') if f.is_file() and f.suffix in ('.key','.crt')}
                assert files and any(k.startswith('acme/') for k in files), 'missing ACME account key'
                if previous is not None:assert previous==files, 'account/certificate keys changed on restart'
                previous=files
            finally:
                p.terminate();p.wait(timeout=25);log.seek(0)
                assert token not in log.read(), 'credential detected in private log; do not publish log'
                assert p.returncode==0
    print('PASS: staging DNS-01 issuance and ACME account/certificate persistence')
print('Renewal requires a separate long-running staging/HA OS acceptance test; not claimed here.')
