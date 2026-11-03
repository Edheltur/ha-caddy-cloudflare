#!/usr/bin/env python3
"""Offline tests against the actual built Caddy and shipped configuration examples.
Test-only Python dependencies never enter the runtime image.
"""
import base64, copy, datetime as dt, hashlib, http.server, json, os
from pathlib import Path
import socket, ssl, struct, subprocess, sys, tempfile, threading, time
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID
import h2.connection, h2.config, h2.events

ROOT = Path(__file__).resolve().parents[1]
BIN = str(Path(sys.argv[1] if len(sys.argv)>1 else ROOT/'caddy-bin').resolve())
TOKEN = 'test_only_' + 'x'*32
ENV = {**os.environ, 'CF_API_TOKEN': TOKEN}
NOW = dt.datetime.now(dt.timezone.utc)

def issue(path, name, ca=None, server=False, expired=False):
    key=ec.generate_private_key(ec.SECP256R1())
    subject=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,name)])
    issuer=ca[1].subject if ca else subject
    b=x509.CertificateBuilder().subject_name(subject).issuer_name(issuer).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(NOW-dt.timedelta(days=10)).not_valid_after(NOW+dt.timedelta(days=-1 if expired else 10)).add_extension(x509.BasicConstraints(ca=ca is None,path_length=0 if ca is None else None),critical=True)
    if ca:
        b=b.add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH if server else ExtendedKeyUsageOID.CLIENT_AUTH]),critical=False)
    if server: b=b.add_extension(x509.SubjectAlternativeName([x509.DNSName(name)]),critical=False)
    cert=b.sign(ca[0] if ca else key,hashes.SHA256())
    (path/(name+'.pem')).write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    (path/(name+'.key')).write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
    return key,cert

class Backend(http.server.BaseHTTPRequestHandler):
    protocol_version='HTTP/1.1'
    def log_message(self,*args): pass
    def do_GET(self):
        if self.headers.get('Upgrade','').lower()=='websocket':
            accept=base64.b64encode(hashlib.sha1((self.headers['Sec-WebSocket-Key']+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
            self.send_response(101);self.send_header('Upgrade','websocket');self.send_header('Connection','Upgrade');self.send_header('Sec-WebSocket-Accept',accept);self.end_headers()
            self.wfile.write(b'\x81\x02ok');self.wfile.flush();self.close_connection=True
        else:
            body=b'upstream-ok';self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)

def connect(port,sni,client=None,tls=None,h2=False):
    ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT);ctx.check_hostname=False;ctx.verify_mode=ssl.CERT_NONE
    if tls: ctx.minimum_version=ctx.maximum_version=tls
    if client: ctx.load_cert_chain(str(P/(client+'.pem')),str(P/(client+'.key')))
    ctx.set_alpn_protocols(['h2'] if h2 else ['http/1.1'])
    return ctx.wrap_socket(socket.create_connection(('127.0.0.1',port),timeout=3),server_hostname=sni)

def request(port,sni,host=None,client=None,tls=None):
    try:
        with connect(port,sni,client,tls) as s:
            s.sendall(f'GET / HTTP/1.1\r\nHost: {host or sni}\r\nConnection: close\r\n\r\n'.encode());data=s.recv(4096)
            return int(data.split(b' ')[1]) if data.startswith(b'HTTP/') else None
    except (ssl.SSLError,ConnectionError):return None

def check(c, detail):
    if not c:raise AssertionError(detail)
    print('PASS',detail,flush=True)

def run(config,name):
    port=18443
    config['apps']['http']['servers']['proxy' if 'proxy' in config['apps']['http']['servers'] else 'srv0']['listen']=['127.0.0.1:'+str(port)]
    file=P/(name+'.json');file.write_text(json.dumps(config))
    log=open(P/(name+'.log'),'w+')
    p=subprocess.Popen([BIN,'run','--config',str(file)],env=ENV,stdout=log,stderr=log)
    for _ in range(100):
        if p.poll() is not None: log.seek(0);raise RuntimeError(log.read())
        try:
            with socket.create_connection(('127.0.0.1',port),.1):break
        except OSError: time.sleep(.05)
    return p,log,port

def stop(p,log):
    p.terminate();p.wait(timeout=20);log.seek(0);text=log.read();log.close()
    check(p.returncode==0,'graceful SIGTERM')
    check(TOKEN not in text,'token absent from logs')

def suite(config,name,strict_unknown=False):
    p,log,port=run(config,name)
    try:
        for tls in (ssl.TLSVersion.TLSv1_2,ssl.TLSVersion.TLSv1_3):
            check(request(port,'ma.example.com',tls=tls)==200,f'{name}: public {tls.name}')
            check(request(port,'ha.example.com',tls=tls) is None,f'{name}: no client rejected {tls.name}')
            for client in ('wrong','expired'):
                check(request(port,'ha.example.com',client=client,tls=tls) is None,f'{name}: {client} rejected {tls.name}')
            check(request(port,'ha.example.com',client='valid',tls=tls)==200,f'{name}: valid client {tls.name}')
            check(request(port,'ma.example.com','ha.example.com',tls=tls)==421,f'{name}: Host mismatch {tls.name}')
        check(request(port,'unknown.example.com') is None,f'{name}: unknown SNI rejected')
        check(request(port,None,'ma.example.com') is None,f'{name}: empty SNI rejected')
        with connect(port,'ma.example.com',h2=True) as s:
            check(s.selected_alpn_protocol()=='h2','HTTP/2 ALPN')
            h=h2.connection.H2Connection(config=h2.config.H2Configuration(client_side=True,header_encoding='utf-8'));h.initiate_connection();s.sendall(h.data_to_send())
            for sid,host,expected in [(1,'ma.example.com',200),(3,'ha.example.com',421)]:
                h.send_headers(sid,[(':method','GET'),(':scheme','https'),(':authority',host),(':path','/')],end_stream=True);s.sendall(h.data_to_send());status=None
                while status is None:
                    data=s.recv(65536)
                    if not data:raise AssertionError('HTTP/2 closed before response')
                    for event in h.receive_data(data):
                        if isinstance(event,h2.events.ResponseReceived) and event.stream_id==sid:status=int(dict(event.headers)[':status'])
                    s.sendall(h.data_to_send())
                check(status==expected,f'HTTP/2 shared connection {host}: {expected}')
        with connect(port,'ha.example.com',client='valid') as s:
            s.sendall(b'GET /api/websocket HTTP/1.1\r\nHost: ha.example.com\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n')
            data=b''
            while b'\r\n\r\n' not in data:data+=s.recv(4096)
            check(data.startswith(b'HTTP/1.1 101'),'WebSocket upgrade')
            while b'\x81\x02ok' not in data:data+=s.recv(4096)
            check(b'\x81\x02ok' in data,'WebSocket frame proxied')
    finally:stop(p,log)
    only13=copy.deepcopy(config)
    for srv in only13['apps']['http']['servers'].values():
        for policy in srv['tls_connection_policies']:policy['protocol_min']='tls1.3'
    p,log,port=run(only13,name+'13')
    try:
        check(request(port,'ma.example.com',tls=ssl.TLSVersion.TLSv1_2) is None,'TLS 1.3-only rejects 1.2')
        check(request(port,'ma.example.com',tls=ssl.TLSVersion.TLSv1_3)==200,'TLS 1.3-only accepts 1.3')
    finally:stop(p,log)

with tempfile.TemporaryDirectory(prefix='caddy-security-') as directory:
    P=Path(directory);ca=issue(P,'client-ca');other=issue(P,'other-ca');serverca=issue(P,'server-ca')
    issue(P,'valid',ca);issue(P,'wrong',other);issue(P,'expired',ca,expired=True)
    for name in ('ha.example.com','ma.example.com','*.example.com'):issue(P,name,serverca,server=True)
    backend=http.server.ThreadingHTTPServer(('127.0.0.1',0),Backend);threading.Thread(target=backend.serve_forever,daemon=True).start()
    upstream='127.0.0.1:'+str(backend.server_port)
    def replace(value):
        return json.loads(json.dumps(value).replace('/config/client-ca.pem',str(P/'client-ca.pem')).replace('/data/caddy',str(P/'state')).replace('homeassistant:8123',upstream).replace('music-assistant:8095',upstream))
    cf=(ROOT/'examples/Caddyfile').read_text().replace('/config/client-ca.pem',str(P/'client-ca.pem')).replace('/data/caddy',str(P/'state'))
    f=P/'Caddyfile';f.write_text(cf)
    subprocess.run([BIN,'validate','--config',str(f),'--adapter','caddyfile'],env=ENV,check=True,capture_output=True)
    adapted=json.loads(subprocess.check_output([BIN,'adapt','--config',str(f),'--adapter','caddyfile'],env=ENV))
    check(adapted['admin']['disabled'],'Caddyfile admin disabled')
    check('cloudflare' in json.dumps(adapted),'DNS module configured')
    for name,config in [('caddyfile',replace(adapted)),('wildcard',replace(json.loads((ROOT/'examples/caddy-wildcard.json').read_text())))]:
        # First validate the actual DNS configuration without issuing certificates.
        f=P/(name+'-validate.json');f.write_text(json.dumps(config))
        subprocess.run([BIN,'validate','--config',str(f)],env=ENV,check=True,capture_output=True)
        certnames=['*.example.com'] if name=='wildcard' else ['ha.example.com','ma.example.com']
        config['apps']['tls']={'certificates':{'load_files':[{'certificate':str(P/(n+'.pem')),'key':str(P/(n+'.key'))} for n in certnames]}}
        for srv in config['apps']['http']['servers'].values():srv['automatic_https']={'disable':True}
        suite(config,name)
    # Offline persistence uses a real Caddy internal CA, not a sentinel file.
    storage=str(P/'persistent')
    internal={'admin':{'disabled':True,'config':{'persist':False}},'storage':{'module':'file_system','root':storage},'apps':{'pki':{'certificate_authorities':{'local':{'install_trust':False}}},'tls':{'certificates':{'automate':['localhost']},'automation':{'policies':[{'subjects':['localhost'],'issuers':[{'module':'internal'}]}]}},'http':{'grace_period':'1s','servers':{'proxy':{'listen':['127.0.0.1:18443'],'protocols':['h1','h2'],'automatic_https':{'disable':True},'tls_connection_policies':[{'match':{'sni':['localhost']}}],'routes':[{'handle':[{'handler':'static_response','body':'ok'}]}]}}}}}
    previous=None
    for n in range(2):
        p,log,port=run(internal,'storage'+str(n))
        try:
            for _ in range(100):
                if request(port,'localhost')==200:break
                time.sleep(.05)
            keys=list(Path(storage).rglob('*.key'))
            check(len(keys)>=3,'internal CA and certificate keys created')
            current={str(f):hashlib.sha256(f.read_bytes()).hexdigest() for f in keys}
            if previous is not None:check(current==previous,'Caddy key storage survives restart')
            previous=current
        finally:stop(p,log)
    backend.shutdown()
print('Offline TLS suite complete. Real DNS issuance and HA OS lifecycle are separate gates.')
