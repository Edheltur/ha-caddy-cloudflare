#!/usr/bin/env python3
"""Root-only launcher test in an empty chroot; --image additionally tests Docker.
Never point this at production /data or /config.
"""
import hashlib,json,os,shutil,signal,socket,subprocess,sys,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
image=sys.argv[2] if len(sys.argv)>2 and sys.argv[1]=='--image' else None
assert os.geteuid()==0, 'run under sudo; test is isolated to a temporary directory'
TOKEN='test_only_'+('x'*32)

def check(condition,message):
    assert condition,message
    print('PASS',message,flush=True)

def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()

with tempfile.TemporaryDirectory(prefix='launcher-test-') as td:
    root=Path(td);root.chmod(0o755)
    for d in ('data','config','usr/bin','dev'): (root/d).mkdir(parents=True,exist_ok=True)
    (root/'data/options.json').write_text(json.dumps({'cloudflare_api_token':TOKEN,'config_format':'json'}))
    # Internal CA exercises real Caddy persistent private-key storage, offline.
    cfg={'admin':{'disabled':True,'config':{'persist':False}},'storage':{'module':'file_system','root':'/data/caddy'},'apps':{'pki':{'certificate_authorities':{'local':{'install_trust':False}}},'tls':{'certificates':{'automate':['localhost']},'automation':{'policies':[{'subjects':['localhost'],'issuers':[{'module':'internal'}]}]}},'http':{'grace_period':'1s','servers':{'test':{'listen':['0.0.0.0:19443'],'protocols':['h1','h2'],'automatic_https':{'disable':True},'tls_connection_policies':[{'match':{'sni':['localhost']}}],'routes':[{'handle':[{'handler':'static_response','body':'ok'}]}]}}}}}
    (root/'config/caddy.json').write_text(json.dumps(cfg))
    if not image:
        shutil.copyfile(ROOT/'launcher-bin',root/'usr/bin/launcher');shutil.copyfile(ROOT/'caddy-bin',root/'usr/bin/caddy')
        for f in (root/'usr/bin').iterdir(): f.chmod(0o755)
    def start():
        if image:
            name='caddy-security-'+str(os.getpid())
            subprocess.run(['docker','run','-d','--name',name,'--cap-drop=NET_RAW','--security-opt=no-new-privileges','-v',str(root/'data')+':/data','-v',str(root/'config')+':/config:ro','-e','SUPERVISOR_TOKEN=must-not-inherit','-e','HASSIO_TOKEN=must-not-inherit',image],check=True,stdout=subprocess.DEVNULL)
            pid=int(subprocess.check_output(['docker','inspect','-f','{{.State.Pid}}',name]))
            return name,pid
        p=subprocess.Popen(['chroot',str(root),'/usr/bin/launcher'],env={**os.environ,'SUPERVISOR_TOKEN':'must-not-inherit','HASSIO_TOKEN':'must-not-inherit'},stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        return p,p.pid
    def stop(handle):
        if image:
            subprocess.run(['docker','stop','-t','10',handle],check=True,stdout=subprocess.DEVNULL)
            log=subprocess.check_output(['docker','logs',handle],stderr=subprocess.STDOUT)
            code=int(subprocess.check_output(['docker','inspect','-f','{{.State.ExitCode}}',handle]))
            subprocess.run(['docker','rm',handle],check=True,stdout=subprocess.DEVNULL)
        else:
            handle.terminate();log=handle.communicate(timeout=10)[0];code=handle.returncode
        if code != 0: print(log.decode(errors='replace').replace(TOKEN,'[redacted]'))
        check(code==0,'launcher exec and SIGTERM exit cleanly')
        check(TOKEN.encode() not in log and b'must-not-inherit' not in log,'runtime logs contain no test secrets')
    hashes=None
    for iteration in range(2):
        handle,pid=start()
        try:
            for _ in range(150):
                keys=list((root/'data/caddy').rglob('*.key'))
                if len(keys)>=3:break
                if not image and handle.poll() is not None: break
                time.sleep(.1)
            check(len(keys)>=3,'Caddy created CA and server private keys')
            status=Path(f'/proc/{pid}/status').read_text()
            fields=dict(line.split(':',1) for line in status.splitlines() if ':' in line)
            check(set(fields['Uid'].split())=={'65532'} and set(fields['Gid'].split())=={'65532'},'Caddy UID/GID 65532')
            check(all(int(fields[k].strip(),16)==0 for k in ['CapInh','CapPrm','CapEff','CapBnd','CapAmb']),'all capability sets empty')
            check(fields['NoNewPrivs'].strip()=='1','no_new_privs enabled')
            # /proc environ is kernel-protected after UID transition; when readable,
            # inspect it. Environment allowlisting is also a launcher unit test.
            try: env=Path(f'/proc/{pid}/environ').read_bytes()
            except PermissionError: env=None
            if env is not None: check(b'must-not-inherit' not in env,'Supervisor tokens absent in Caddy environment')
            for key in keys:check(key.stat().st_mode & 0o777==0o600,'private key mode 0600')
            now={str(p.relative_to(root)):digest(p) for p in keys}
            if hashes is not None:check(now==hashes,'private key storage reused after restart')
            hashes=now
        finally:stop(handle)
    # Malformed credentials must never reach the module's unsafe error message.
    (root/'data/options.json').write_text('{"cloudflare_api_token":"MALFORMED-SECRET","config_format":"json"}')
    if not image:
        result=subprocess.run(['chroot',str(root),'/usr/bin/launcher'],capture_output=True)
        check(result.returncode!=0 and b'MALFORMED-SECRET' not in result.stderr,'invalid token rejected without disclosure')
print('Runtime checks complete; this is not a substitute for testing Supervisor mounts/AppArmor.')
