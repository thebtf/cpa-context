"""Run with local binaries. --ui opens Fleet in headless Chrome for single-instance families."""
import contextlib
import http.client
import http.server
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import argparse
import urllib.parse
import urllib.request
import uuid

EXPECTED = {'ROOTTEST': None, 'CHILDALPHA': 'ROOTTEST', 'CHILDBETA': 'ROOTTEST', 'GRANDLEAF': 'CHILDALPHA'}

def role(body):
    messages = body.get('messages', body.get('input', []))
    text = str([m for m in messages if isinstance(m, dict) and m.get('role') == 'user'])
    return next((k for k in reversed(EXPECTED) if 'LINEAGE_' + k in text), None)

class FixtureServer(http.server.ThreadingHTTPServer):
    lock: threading.Lock
    seen: set
    branch_model: str | None
    marker: str
    probes: list
    captured: list
    arrived: set
    barrier: threading.Barrier | None
    target: int

class HTTP(http.server.BaseHTTPRequestHandler):
    @property
    def fixture(self) -> FixtureServer:
        assert isinstance(self.server, FixtureServer)
        return self.server

    def log_message(self, format, *args): pass
    def send(self, status, mime, raw):
        self.send_response(status); self.send_header('Content-Type', mime); self.send_header('Content-Length', str(len(raw))); self.end_headers(); self.wfile.write(raw)
    def do_GET(self): self.send(200, 'application/json', b'{"data":[]}')

class Provider(HTTP):
    def do_POST(self):
        b = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        names = {t['function']['name'].lstrip('_'): t['function']['name'] for t in b.get('tools', []) if 'function' in t}
        kind = role(b); turn = (kind, bool(names))
        with self.fixture.lock:
            first = turn not in self.fixture.seen; self.fixture.seen.add(turn)
        name = None; args = {}
        if first and kind in ('ROOTTEST', 'CHILDALPHA') and 'task' in names:
            name = names['task']; children = ['CHILDALPHA', 'CHILDBETA'] if kind == 'ROOTTEST' else ['GRANDLEAF']
            args = {'context': 'local fixture', 'tasks': [{'name': c, 'agent': 'brancher' if c == 'CHILDALPHA' else 'task', 'task': 'return LINEAGE_' + c, 'solutionSpace': 'return one token'} for c in children]}
            if self.fixture.branch_model:
                for item in args['tasks']:
                    item['model'] = self.fixture.branch_model if item['name'] == 'CHILDALPHA' else 'stand/lineage'
        elif first and kind not in ('ROOTTEST', None) and 'yield' in names:
            name = names['yield']; args = {'result': 'OK'}
        delta = {'role': 'assistant', 'content': 'OK'} if name is None else {'role': 'assistant', 'tool_calls': [{'index': 0, 'id': 'call_' + uuid.uuid4().hex[:10], 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(args)}}]}
        def frame(d, finish):
            return 'data: ' + json.dumps({'id': 'chatcmpl-local', 'object': 'chat.completion.chunk', 'created': 12345678, 'model': b['model'], 'choices': [{'index': 0, 'delta': d, 'finish_reason': finish}], 'usage': {'prompt_tokens': 100, 'completion_tokens': 4, 'total_tokens': 104}}) + '\n\n'
        self.send(200, 'text/event-stream', (frame(delta, None) + frame({}, 'tool_calls' if name else 'stop') + 'data: [DONE]\n\n').encode())

class Wire(HTTP):
    def do_GET(self):
        s = self.fixture
        if self.path != '/gateway/':
            return self.send(404, 'text/plain', b'')
        s.probes.append((dict(self.headers), self.rfile.read(int(self.headers.get('Content-Length', '0')))))
        if s.marker == 'redirect':
            self.send_response(302); self.send_header('Location', f'http://127.0.0.1:{s.target}/'); self.end_headers(); return
        if s.marker == 'other':
            return self.send(200, 'application/json', b'{"message":"Other API"}')
        with contextlib.closing(http.client.HTTPConnection('127.0.0.1', s.target, timeout=5)) as con:
            con.request('GET', '/'); response = con.getresponse()
            self.send(response.status, response.getheader('Content-Type'), response.read())

    def do_POST(self):
        s = self.fixture; data = self.rfile.read(int(self.headers['Content-Length'])); b = json.loads(data)
        sid = self.headers.get('X-Claude-Code-Session-Id') or self.headers.get('X-Session-Id')
        parent = self.headers.get('X-Parent-Session-Id'); kind = role(b); main = bool(b.get('tools') and kind)
        with s.lock:
            s.captured.append((kind, sid, parent, main, self.path))
            wait = main and kind in ('CHILDALPHA', 'CHILDBETA') and kind not in s.arrived
            if wait: s.arrived.add(kind)
        if wait and s.barrier: s.barrier.wait(12)
        with contextlib.closing(http.client.HTTPConnection('127.0.0.1', s.target, timeout=50)) as con:
            headers = {k: v for k, v in self.headers.items() if k.lower() not in ('connection', 'host', 'accept-encoding')}
            con.request('POST', self.path.removeprefix('/gateway'), data, headers); res = con.getresponse()
            self.send(res.status, res.getheader('Content-Type', 'application/json'), res.read())

@contextlib.contextmanager
def serve(handler):
    with FixtureServer(('127.0.0.1', 0), handler) as s:
        s.lock = threading.Lock(); t = threading.Thread(target=s.serve_forever, daemon=True); t.start()
        try: yield s
        finally: s.shutdown(); t.join()

CLIENT = 'stand-client'
KEY = 'stand-management'
MODEL = 'lineage'

def isolated_env(home):
    inherited = {'PATH', 'PATHEXT', 'SYSTEMROOT', 'WINDIR', 'SYSTEMDRIVE', 'COMSPEC', 'PROCESSOR_ARCHITECTURE', 'NUMBER_OF_PROCESSORS'}
    env = {k: v for k, v in os.environ.items() if k.upper() in inherited}
    env.update({k: str(home) for k in ('HOME','USERPROFILE','LOCALAPPDATA','APPDATA','TMP','TEMP','TMPDIR')})
    env.update(PI_CODING_AGENT_DIR=str(home/'profile'), NO_PROXY='127.0.0.1,localhost', HTTP_PROXY='http://127.0.0.1:1', HTTPS_PROXY='http://127.0.0.1:1', NO_COLOR='1')
    return env

@contextlib.contextmanager
def instance(root, cpa, library, upstream, env):
    system = {'Windows':'windows', 'Linux':'linux', 'Darwin':'darwin'}[platform.system()]
    arch = 'arm64' if platform.machine().lower() in ('arm64','aarch64') else 'amd64'
    plugins = root/'plugins'/system/arch
    plugins.mkdir(parents=True); (root/'data').mkdir(); (root/'auths').mkdir()
    shutil.copyfile(library, plugins/library.name)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1',0)); port = sock.getsockname()[1]
    cfg = {'host':'127.0.0.1', 'port':port, 'auth-dir':(root/'auths').as_posix(), 'remote-management':{'secret-key':KEY},
           'access':{'api-keys':[CLIENT]}, 'api-keys':{'openai-compatibility':[{'name':'fixture', 'base-url':f'http://127.0.0.1:{upstream}/v1',
           'keys':[{'api-key':'local'}],'models':[{'name':MODEL,'alias':MODEL}]}]}, 'usage-statistics-enabled':True,
           'plugins':{'enabled':True,'dir':(root/'plugins').as_posix(),'configs':{'cpa-context':{'enabled':True,'data_dir':(root/'data').as_posix()}}}}
    config=root/'config.yaml'; config.write_text(json.dumps(cfg),encoding='utf8')
    with open(root/'cpa.log','w',encoding='utf8') as log:
        proc=subprocess.Popen([str(cpa),'--config',str(config)],cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT,creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
        try:
            for _ in range(100):
                try: management(port,'sessions'); break
                except OSError: time.sleep(.15)
            else: raise RuntimeError('local CPA/plugin did not start')
            yield port
        finally:
            proc.terminate(); proc.wait(timeout=10)


def management(port,path):
    request=urllib.request.Request(f'http://127.0.0.1:{port}/v0/management/cpa-context/{path}', headers={'Authorization':'Bearer '+KEY})
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=6) as res: return json.load(res)


def write_profile(profile, endpoints):
    profile.mkdir(); (profile/'agents').mkdir()
    branch_model = 'model: second/lineage\n' if 'second' in endpoints else ''
    (profile/'agents'/'brancher.md').write_text('---\nname: brancher\ndescription: local fixture\ntools: [task,yield]\nspawns: "*"\n'+branch_model+'---\nReturn the assignment.\n', encoding='utf8')
    cfg={'modelRoles':{k:'stand/lineage' for k in ['default','smol','slow','task','scout','plan']},
         'task':{'batch':True, 'maxDepth':3, 'perCallModel':'on', 'perCallModelAllow':[f'{name}/{MODEL}' for name in endpoints]},
         'async':{'enabled':False},'retry':{'enabled':False}}
    (profile/'config.yml').write_text(json.dumps(cfg), encoding='utf8')
    model={'id':MODEL,'name':MODEL,'reasoning':False,'input':['text'],'contextWindow':256000,'maxTokens':6000}
    providers={k:{'baseUrl':f'http://127.0.0.1:{port}/gateway/v1','api':api,'apiKey':CLIENT,'models':[model]} for k,(port,api) in endpoints.items()}
    (profile/'models.yml').write_text(json.dumps({'providers': providers}), encoding='utf8')


def run_case(name, omp, cpa, library, tmp, out, ui):
    split=name=='split'; mixed=split or name=='mixed'; negative=name in ('control','redirect','other');
    env=isolated_env(tmp); cwd=tmp/'work'; cwd.mkdir(); profile=tmp/'profile'
    with contextlib.ExitStack() as stack:
        overlap=threading.Event(); barrier=threading.Barrier(2, action=overlap.set); arrived=set(); lock=threading.Lock()
        hosts=[];wires=[]
        for i in range(2 if split else 1):
            port=stack.enter_context(instance(tmp/str(i),cpa,library,out.server_port,env)); hosts.append(port)
            wire=stack.enter_context(serve(Wire)); wires.append(wire)
            wire.target=port;wire.marker=name;wire.probes=[];wire.captured=[];wire.barrier=barrier;wire.arrived=arrived;wire.lock=lock
        api={'claude':'anthropic-messages','responses':'openai-responses'}.get(name,'openai-completions')
        endpoints={'stand':(wires[0].server_port,api)}
        if mixed: endpoints['second']=(wires[-1].server_port,'anthropic-messages')
        write_profile(profile,endpoints)
        if name!='control':
            ext=profile/'extensions'; ext.mkdir(); shutil.copyfile(Path(__file__).resolve().parents[1]/'extensions/omp-task-lineage.ts',ext/'lineage.ts')
        out.seen=set();out.branch_model='second/lineage' if mixed else None
        run=subprocess.run([str(omp),'-p',*([] if mixed else ['--offline']),*(['--no-session'] if name=='ephemeral' else []),'--no-lsp','--no-pty','--no-rules','--no-skills','--no-context-files','--no-title','--tools','task,wait,yield','--model','stand/lineage','--smol','stand/lineage','--slow','stand/lineage','--max-time','65','Process LINEAGE_ROOTTEST.'],cwd=cwd,env=env,capture_output=True,text=True,errors='replace',timeout=80,creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
        assert run.returncode==0 and 'Failed to load extension' not in run.stderr,(name,run.stderr[-1200:])
        turns=[(i,k,s,p,r) for i,w in enumerate(wires) for k,s,p,main,r in w.captured if main]
        ids={k:s for _,k,s,_,_ in turns};assert set(ids)==set(EXPECTED) and len(set(ids.values()))==4 and overlap.is_set(),(name,turns)
        rows=[{r['id'].partition(':')[2]:r for r in management(port,'sessions')['sessions']} for port in hosts]
        for w in wires:
            assert bool(w.probes)==(name!='control'),(name,'missing or unexpected discovery')
            for headers,body in w.probes:
                assert not body and not {'authorization','proxy-authorization','x-api-key','cookie','x-session-id','x-parent-session-id'}.intersection(k.lower() for k in headers)
            assert all(p is None for _,_,p,main,_ in w.captured if not main)
        for i,k,s,p,route in turns:
            assert i==int(split and k=='CHILDALPHA'),(name,k,i)
            assert not any(s in db for idx,db in enumerate(rows) if idx!=i),(name,k,'unexpected database')
            expected=None if negative else ids.get(EXPECTED[k]);row=rows[i][s]; got=row.get('parentId')
            assert p==expected,(name,k,p,expected)
            if split:
                # The parent lives in the other instance's database, so only the id
                # CPA derived from can be compared here.
                assert (got.partition(':')[2] if got else None)==expected,(name,k,p,got,expected)
            else:
                # Both rows are in one database: the graph must name the row the
                # dashboard shows, so a transport prefix that disagrees is a broken
                # edge even when the id behind it matches.
                visible=None if expected is None else rows[i][ids[EXPECTED[k]]]['id']
                assert got==visible,(name,k,p,got,visible)
            assert row['origin']==('subagent' if expected else 'root'),(name,k,row['origin'])
            if mixed: assert ('/messages' in route)==(k=='CHILDALPHA'),(name,k,route)
        if not negative and not split:
            actual=[rows[0][ids[k]]['id'] for k in EXPECTED]; roster=management(hosts[0],'session?'+urllib.parse.urlencode({'session':actual[0]}))['agentTeam']['members']
            assert {m['id'] for m in roster}==set(actual),(name,roster)
            if ui and name in ('claude','mixed'):
                dashboard=f'http://127.0.0.1:{hosts[0]}/v0/resource/plugins/cpa-context/dashboard'
                subprocess.run([ui,'test/omp-lineage-ui.mjs',dashboard,KEY,*actual],check=True,cwd=Path(__file__).resolve().parents[1],timeout=100)
        counts=[sum(s in db for s in ids.values()) for db in rows]
        print(f'{name}: conversations by CPA={counts}, parent edges={0 if negative else 3}, roles correct, siblings overlapped',flush=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    for key in ('omp','cpa','library'): parser.add_argument(key,type=Path)
    parser.add_argument('--only',action='append',choices=['control','chat','claude','responses','mixed','ephemeral','redirect','other','split'])
    parser.add_argument('--ui',action='store_true');args=parser.parse_args(); ui=shutil.which('node') if args.ui else None
    if args.ui and not ui: parser.error('node is required for --ui')
    with serve(Provider) as out:
        for name in args.only or ['control','chat','claude','responses','mixed','ephemeral','redirect','other','split']:
            with tempfile.TemporaryDirectory(prefix='lineage-',dir=Path(__file__).resolve().parent) as folder:
                run_case(name,args.omp.resolve(),args.cpa.resolve(),args.library.resolve(),Path(folder),out,ui)
