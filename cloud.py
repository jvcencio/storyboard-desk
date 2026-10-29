"""SBD-owned, explicit Google generation jobs through OpenRouter. No automatic spending."""
import base64
import copy
import datetime as dt
import hashlib
import importlib.util
import io
import json
import math
import mimetypes
import os
from pathlib import Path
import queue
import re
import secrets
import shutil
import threading
import urllib.error
import urllib.parse
import urllib.request
from platform_support import FileLock

APP = Path(__file__).resolve().parent
API = 'https://openrouter.ai/api/v1/'
IMAGE_MODELS = ('google/gemini-3-pro-image', 'google/gemini-3.1-flash-image', 'google/gemini-nano-banana-2.1')
VIDEO_MODELS = ('google/veo-3.1-fast', 'google/veo-3.1-lite', 'google/veo-3.1')
ACTIVE = ('queued', 'submitting', 'pending', 'in_progress', 'downloading', 'importing')

def now(): return dt.datetime.now(dt.timezone.utc).isoformat()
def uid(): return secrets.token_hex(6)
def digest(value): return hashlib.sha256(json.dumps(value,sort_keys=True).encode()).hexdigest()
def atomic(path, value):
    path.parent.mkdir(parents=True,exist_ok=True)
    temp=path.with_suffix('.'+uid()+'.tmp')
    with temp.open('x', encoding='utf-8') as f:
        json.dump(value,f,indent=2);f.flush()
        import os
        os.fsync(f.fileno())
    temp.chmod(0o600);temp.replace(path)

def keychain_key():
    key = os.environ.get('OPENROUTER_API_KEY', '').strip()
    if key:
        return key
    path=APP.parent/'buckley-workstation/scripts/workstation_credentials.py'
    if not path.is_file():
        raise ValueError('Start with python3 launch.py --ask-openrouter-key, or set OPENROUTER_API_KEY in the server environment.')
    spec=importlib.util.spec_from_file_location('sbd_workstation_credentials',path)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    key,_=module.find('openrouter')
    if not key: raise ValueError('OpenRouter needs secure setup in the workstation Keychain.')
    return key

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs): return None

class Client:
    """Credentials never leave the pinned API origin or appear in saved jobs/errors."""
    def __init__(self,key_loader=keychain_key):
        self.key_loader=key_loader
        self.opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    def request(self,path,body=None,auth=True,binary=False):
        if not re.fullmatch(r'(?:auth/key|images(?:/models)?|videos(?:/models|/[A-Za-z0-9_-]+(?:/content)?)?)',path):
            raise ValueError('Unsupported provider operation.')
        headers={'Content-Type':'application/json','User-Agent':'StoryboardDesk/0.7'}
        if auth: headers['Authorization']='Bearer '+self.key_loader()
        data=json.dumps(body).encode() if body is not None else None
        req=urllib.request.Request(API+path,data=data,headers=headers)
        try:
            with self.opener.open(req,timeout=240 if path=='images' else 60) as r:
                raw=r.read(256*1024*1024+1)
                if len(raw)>256*1024*1024: raise ValueError('Provider result exceeds the local media limit.')
                return raw if binary else json.loads(raw)
        except urllib.error.HTTPError as e:
            # Do not echo provider messages that could contain prompts or request data.
            raise ValueError(f'OpenRouter HTTP {e.code}. Check account access and model availability; no automatic resubmission.') from None
        except (urllib.error.URLError,TimeoutError,ConnectionError,OSError):
            raise RuntimeError('Provider connection interrupted. The submission outcome may be unknown; do not resubmit blindly.') from None
    def check(self): self.request('auth/key'); return True
    def catalogs(self):
        return {'observed_at':now(),'images':self.request('images/models',auth=False)['data'],
                'videos':self.request('videos/models',auth=False)['data']}
    def image(self,body): return self.request('images',body)
    def video(self,body): return self.request('videos',body)
    def poll(self,rid): return self.request('videos/'+rid)
    def download(self,rid): return self.request('videos/'+rid+'/content',binary=True)

class CloudEngine:
    def __init__(self,store,approved,basis,prompts,client=None,probe=None,start_worker=True):
        self.store,self.approved,self.basis,self.prompts=store,approved,basis,prompts
        self.client=client or Client();self.probe=probe
        self.root=store.root/'cloud';self.lock=threading.RLock();self.stop=threading.Event()
        self.queue=queue.Queue();self.connected=None;self.connection_message='Check the saved OpenRouter connection before generating.'
        self.worker_lock=None
        if start_worker:
            self.root.mkdir(parents=True,exist_ok=True)
            self.worker_lock=FileLock(self.root/'worker.lock')
            if not self.worker_lock.acquire():
                self.worker_lock.close()
                raise ValueError('Another SBD generation worker already owns this data folder.') from None
        self.catalog=json.loads((APP/'cloud_catalog.json').read_text(encoding='utf-8'))
        if (self.root/'catalog.json').exists(): self.catalog=json.loads((self.root/'catalog.json').read_text(encoding='utf-8'))
        self.jobs={};self.worker=None
        for path in self.root.glob('jobs/*.json'):
            j=json.loads(path.read_text(encoding='utf-8'));self.jobs[j['id']]=j
            if j['status'] in ACTIVE:
                if j.get('remote_id') or j.get('result_file') or j['status']=='queued': self.queue.put(j['id'])
                else:
                    j.update(status='unknown',message='Interrupted during submission. Reconcile provider activity before creating another request.');self.save(j)
        if start_worker:
            self.worker=threading.Thread(target=self.run,daemon=True);self.worker.start()
    def save(self,j):
        atomic(self.root/'jobs'/(j['id']+'.json'),j)
    def config(self,pid):
        self.store.load(pid)
        path=self.store.folder(pid)/'cloud/config.json'
        return json.loads(path.read_text(encoding='utf-8')) if path.exists() else {'allowance_usd':0}
    def spent(self,pid):
        return sum(j.get('actual_cost_usd') if j.get('actual_cost_usd') is not None else j['reserved_usd']
                   for j in self.jobs.values() if j['project_id']==pid and j['status']!='cancelled')
    def allowance(self,pid,value):
        value=float(value)
        if not math.isfinite(value) or not 0<=value<=1000: raise ValueError('Use a total project allowance between $0 and $1,000.')
        with self.lock:
            if value+1e-6<self.spent(pid): raise ValueError('The allowance cannot be below existing charges and reservations.')
            atomic(self.store.folder(pid)/'cloud/config.json',{'allowance_usd':value,'at':now()})
        return self.status(pid)
    def brief(self,pid):
        path=self.store.folder(pid)/'cloud/production-brief.json'
        return json.loads(path.read_text(encoding='utf-8')) if path.exists() else None
    def approve_brief(self,pid,sha):
        with self.lock:
            b=self.brief(pid)
            if not b or digest(b['content'])!=sha: raise RuntimeError('The production brief changed. Review it again.')
            b.update(approved_at=now(),approved_sha256=sha)
            atomic(self.store.folder(pid)/'cloud/production-brief.json',b)
        return self.status(pid)
    def status(self,pid):
        with self.lock:
            self.store.load(pid)
            return {'connected':self.connected,'connection_message':self.connection_message,
                    'catalog_observed_at':self.catalog['observed_at'],
                    'image_models':[m for m in self.catalog['images'] if m['id'] in IMAGE_MODELS],
                    'video_models':[m for m in self.catalog['videos'] if m['id'] in VIDEO_MODELS],
                    'allowance_usd':self.config(pid)['allowance_usd'],'committed_usd':round(self.spent(pid),4),
                    'jobs':sorted([copy.deepcopy(j) for j in self.jobs.values() if j['project_id']==pid],key=lambda j:j['created_at'],reverse=True),
                    'brief':self.brief(pid)}
    def connect(self,pid):
        try:
            self.client.check();catalog=self.client.catalogs()
            self.catalog=catalog;atomic(self.root/'catalog.json',catalog)
            self.connected=True;self.connection_message='OpenRouter connected. Google model catalog refreshed; generation still needs a reviewed request.'
        except (ValueError,RuntimeError,OSError):
            self.connected=False;self.connection_message='Could not verify OpenRouter. Check the server API key or existing Keychain connection and account availability.'
            raise
        return self.status(pid)
    def images(self,p):
        rows=[dict(r,label='Reference · '+r['title']) for r in p.get('visual_references',[])]
        for s in p['shots']:
            for a in s['assets']:
                if a.get('binned_at'): continue
                if a['kind'] in ('sketch','keyframe'):
                    rows.append(dict(a,label=s['title']+' · '+a['kind']+' take '+str(a['take'])))
                elif a['kind']=='clip' and a.get('poster_path') and not a.get('source_ref'):
                    rows.append({'id':'poster-'+a['id'],'path':a['poster_path'],'sha256':a.get('poster_sha256'),
                                 'label':s['title']+' · clip '+str(a['take'])+' poster (layout reference)'})
        return rows
    def input(self,p,iid):
        row=next((r for r in self.images(p) if r['id']==iid),None)
        if not row: raise ValueError('Choose an active image from this project.')
        root=self.store.folder(p['id']).resolve();path=(root/row['path']).resolve()
        if not path.is_relative_to(root) or not row['path'].startswith('media/') or not path.is_file(): raise ValueError('Reference image is unavailable.')
        if path.suffix.lower() not in ('.png','.jpg','.jpeg','.webp') or not 0<path.stat().st_size<=10*1024*1024: raise ValueError('Use a PNG, JPEG or WebP reference smaller than 10 MiB.')
        sha=hashlib.sha256(path.read_bytes()).hexdigest()
        if row.get('sha256') and row['sha256']!=sha: raise ValueError('Reference bytes changed. Import the image again.')
        return {'id':iid,'path':row['path'],'sha256':sha,'title':row['label']}
    def prepare(self,raw):
        p=self.store.load(raw['project']);s=self.store.shot(p,raw['shot'])
        if p['revision']!=raw['revision']: raise RuntimeError('Project changed. Reload before preparing generation.')
        kind=raw.get('kind');model=raw.get('model');prompt=raw.get('prompt','')
        if kind not in ('image','video') or not isinstance(prompt,str) or not 1<=len(prompt.strip())<=12000: raise ValueError('Write a prompt of 1–12,000 characters.')
        prompt=prompt.strip()
        allowed=IMAGE_MODELS if kind=='image' else VIDEO_MODELS
        if model not in allowed: raise ValueError('Choose a supported Google model.')
        m=next((m for m in self.catalog['images' if kind=='image' else 'videos'] if m['id']==model),None)
        if not m: raise ValueError('Model absent from the catalog. Check connection to refresh it.')
        q={'id':uid(),'project_id':p['id'],'shot_id':s['id'],'revision':p['revision'],'kind':kind,'model':model,'prompt':prompt,
           'aspect':p['aspect'],'basis':self.basis(p,s),'created_at':now(),'refs':[], 'starter_id':None,'catalog_sha256':digest(m)}
        if kind=='image':
            ids=raw.get('reference_ids',[])
            if not isinstance(ids,list) or len(ids)>6 or len(set(ids))!=len(ids): raise ValueError('Choose up to six distinct reference images.')
            params=m.get('supported_parameters',{})
            if p['aspect'] not in params.get('aspect_ratio',{}).get('values',[]) or '1K' not in params.get('resolution',{}).get('values',[]): raise ValueError('This image model does not support the requested frame.')
            q['refs']=[self.input(p,i) for i in ids];q.update(resolution='1K',reserved_usd=1.0,price_note='Conservative $1 image allowance; token-based actual cost is reported afterward. This is not a provider-enforced maximum charge.')
        else:
            if not self.approved(p,s): raise ValueError('Select and approve a finished starter image before generating video.')
            duration=raw.get('duration',8);resolution=raw.get('resolution','720p')
            if isinstance(duration,bool) or duration not in m.get('supported_durations',[]) or resolution not in m.get('supported_resolutions',[]) or p['aspect'] not in m.get('supported_aspect_ratios',[]) or m.get('generate_audio') is not True or 'first_frame' not in m.get('supported_frame_images',[]): raise ValueError('The selected model does not advertise this silent frame/duration combination.')
            skus=m.get('pricing_skus',{});suffix='_720p' if resolution=='720p' else '_4k' if resolution=='4K' else ''
            price=skus.get('duration_seconds_without_audio'+suffix,skus.get('duration_seconds_without_audio'))
            if price is None: raise ValueError('Silent-video pricing is unavailable; refresh the catalog.')
            cost=float(price)*duration
            if not math.isfinite(cost) or cost<=0: raise ValueError('Silent-video pricing is invalid.')
            q.update(duration=duration,resolution=resolution,reserved_usd=round(cost,4),generate_audio=False,
                     starter_id=s['selected_keyframe'],refs=[self.input(p,s['selected_keyframe'])],
                     price_note='Listed silent-video estimate. Actual cost is reconciled when reported.')
        brief=self.brief(p['id']);q['brief_sha256']=digest(brief['content']) if brief else None
        if q['refs']:
            q['prompt']+='\n\nINPUT IMAGE GUIDE\n'+'\n'.join(f"Image {i+1}: {r['title']}" for i,r in enumerate(q['refs']))
        atomic(self.root/'quotes'/(q['id']+'.json'),q)
        return q
    def submit(self,qid,confirmed=False):
        if not confirmed: raise ValueError('Review the request and confirm generation explicitly.')
        if not re.fullmatch('[a-f0-9]{12}',qid): raise ValueError('Invalid prepared request.')
        with self.lock,self.store.lock:
            if self.connected is not True: raise ValueError('Check the saved OpenRouter connection first.')
            q=json.loads((self.root/'quotes'/(qid+'.json')).read_text(encoding='utf-8'))
            if qid in self.jobs: return copy.deepcopy(self.jobs[qid])
            age=(dt.datetime.now(dt.timezone.utc)-dt.datetime.fromisoformat(q['created_at'])).total_seconds()
            if not 0<=age<=900: raise RuntimeError('The request expired. Prepare it again.')
            p=self.store.load(q['project_id']);s=self.store.shot(p,q['shot_id'])
            models=self.catalog['images' if q['kind']=='image' else 'videos']
            model=next((m for m in models if m['id']==q['model']),None)
            if digest(model)!=q['catalog_sha256']: raise RuntimeError('Model capabilities or prices changed. Prepare a fresh request.')
            if p['revision']!=q['revision'] or self.basis(p,s)!=q['basis']: raise RuntimeError('The project or starter changed. Prepare a fresh request.')
            for ref in q['refs']:
                if self.input(p,ref['id'])['sha256']!=ref['sha256']: raise RuntimeError('An input changed. Prepare again.')
            b=self.brief(p['id'])
            if q['brief_sha256'] and (not b or b.get('approved_sha256')!=q['brief_sha256'] or digest(b['content'])!=q['brief_sha256']): raise ValueError('Review and approve the production brief before generating.')
            if self.spent(p['id'])+q['reserved_usd']>self.config(p['id'])['allowance_usd']+1e-6: raise ValueError('This request exceeds the remaining project allowance. Set an allowance first.')
            if any(j['project_id']==p['id'] and j['shot_id']==s['id'] and j['status'] in ACTIVE for j in self.jobs.values()): raise ValueError('This shot already has an active cloud job.')
            # Keychain lookup remains server-side and precedes any job/spend commitment.
            if isinstance(self.client,Client): self.client.key_loader()
            j=dict(q,status='queued',message='Queued for Google generation through OpenRouter.',actual_cost_usd=None)
            folder=self.root/'inputs'/qid;folder.mkdir(parents=True,exist_ok=True)
            for n,r in enumerate(j['refs']):
                src=self.store.folder(p['id'])/r['path'];target=folder/(str(n)+src.suffix.lower());shutil.copyfile(src,target)
                if hashlib.sha256(target.read_bytes()).hexdigest()!=r['sha256']: raise ValueError('Input changed during snapshot.')
                r['snapshot']=target.relative_to(self.root).as_posix()
            self.save(j);self.jobs[qid]=j;self.queue.put(qid)
            return copy.deepcopy(j)
    def resume(self,jid):
        with self.lock:
            j=self.jobs[jid]
            if j['status'] in ACTIVE: return copy.deepcopy(j)
            if not j.get('remote_id') and not j.get('result_file'): raise ValueError('No recoverable provider ID/result. Reconcile activity before creating another job.')
            j.update(status='pending' if j.get('remote_id') else 'importing',message='Checking the existing job; no new generation request.')
            self.save(j);self.queue.put(jid);return copy.deepcopy(j)
    def body(self,j):
        def image_ref(r):
            path=self.root/r['snapshot'];data=path.read_bytes()
            if hashlib.sha256(data).hexdigest()!=r['sha256']: raise ValueError('Saved generation input changed.')
            return {'type':'image_url','image_url':{'url':'data:'+mimetypes.guess_type(path.name)[0]+';base64,'+base64.b64encode(data).decode()}}
        body={'model':j['model'],'prompt':j['prompt'],'aspect_ratio':j['aspect'],'resolution':j['resolution']}
        if j['kind']=='image': body.update(n=1,input_references=[image_ref(r) for r in j['refs']])
        else: body.update(duration=j['duration'],generate_audio=False,frame_images=[dict(image_ref(j['refs'][0]),frame_type='first_frame')])
        return body
    def update(self,j,**values):
        with self.lock: j.update(values);self.save(j)
    def process(self,j):
        folder=self.root/'results'/j['id'];folder.mkdir(parents=True,exist_ok=True)
        if j.get('asset_id'):
            self.update(j,status='done',message='Take already imported. Review before selecting.');return
        if not j.get('result_file'):
            if j['kind']=='image':
                self.update(j,status='submitting',message='Creating the reference-conditioned image.')
                result=self.client.image(self.body(j));data=result.get('data',[])
                if len(data)!=1: raise RuntimeError('Unexpected image result count; reconcile provider activity.')
                item=data[0];ext={'image/png':'.png','image/jpeg':'.jpg','image/webp':'.webp'}.get(item.get('media_type','image/png'))
                if not ext: raise RuntimeError('Unsupported generated image format; result needs manual review.')
                payload=base64.b64decode(item['b64_json'],validate=True);path=folder/('image'+ext);path.write_bytes(payload)
                self.update(j,result_file=path.relative_to(self.root).as_posix(),status='importing',actual_cost_usd=self.cost(result))
            else:
                if not j.get('remote_id'):
                    self.update(j,status='submitting',message='Submitting one silent Veo take.')
                    result=self.client.video(self.body(j));rid=result.get('id','')
                    if not isinstance(rid,str) or not re.fullmatch('[A-Za-z0-9_-]{1,200}',rid): raise RuntimeError('Provider returned no recoverable job ID. Reconcile activity.')
                    self.update(j,remote_id=rid,status='pending',message='Veo is working; this job can resume after restart.')
                while not self.stop.is_set():
                    result=self.client.poll(j['remote_id']);status=result.get('status')
                    if status=='completed':
                        self.update(j,status='downloading',actual_cost_usd=self.cost(result),message='Downloading the completed take.')
                        payload=self.client.download(j['remote_id']);path=folder/'video.mp4';path.write_bytes(payload)
                        self.update(j,result_file=path.relative_to(self.root).as_posix(),status='importing');break
                    if status=='failed':
                        self.update(j,status='failed',message='Provider reported failure. Inspect account activity before creating another take.',actual_cost_usd=self.cost(result));return
                    if status not in ('pending','in_progress'): raise RuntimeError('Provider returned an unfamiliar job status. Check this existing job again.')
                    self.update(j,status=status,message='Veo is generating the silent take.')
                    self.stop.wait(10)
                if self.stop.is_set(): return
        path=self.root/j['result_file'];metadata=None
        if j['kind']=='video':
            if not self.probe: raise ValueError('Video inspection tool is unavailable; download retained for later import.')
            metadata=self.probe(path,folder)
            if metadata.get('audio_tracks')!=0: raise ValueError('The requested silent take contains audio or could not be verified. Saved for investigation; not imported.')
            if not 0<float(metadata.get('duration_seconds',0))<=7200: raise ValueError('Generated clip duration could not be verified.')
        self.import_result(j,path,metadata)
    @staticmethod
    def cost(result):
        value=result.get('usage',{}).get('cost')
        try:
            value=float(value)
            return value if math.isfinite(value) and value>=0 else None
        except (TypeError,ValueError): return None
    def import_result(self,j,path,metadata=None):
        with self.lock,self.store.lock:
            p=self.store.load(j['project_id']);s=self.store.shot(p,j['shot_id'])
            prior=next((a for a in s['assets'] if a.get('cloud_generation',{}).get('job_id')==j['id']),None)
            provenance={k:copy.deepcopy(j[k]) for k in ('id','model','kind','prompt','refs','basis','starter_id','generate_audio','duration','resolution','actual_cost_usd','remote_id') if k in j}
            provenance['job_id']=provenance.pop('id');provenance['direction_stale_on_return']=self.basis(p,s)!=j['basis']
            if not prior:
                with path.open('rb') as f:
                    p=self.store.import_asset(p['id'],s['id'],'keyframe' if j['kind']=='image' else 'clip',path.name,f,path.stat().st_size,'OpenRouter · Google',cloud_provenance=provenance)
                s=self.store.shot(p,j['shot_id']);a=s['assets'][-1]
            else: a=prior
            if metadata:
                a.update(duration_seconds=metadata['duration_seconds'],in_seconds=0,out_seconds=metadata['duration_seconds'],audio_tracks=0)
                poster=Path(metadata.get('poster',''))
                if poster.is_file():
                    rel='media/'+s['id']+'/'+a['id']+'-poster.jpg';shutil.copyfile(poster,self.store.folder(p['id'])/rel)
                    a.update(poster_path=rel,poster_sha256=hashlib.sha256(poster.read_bytes()).hexdigest())
                self.store.save(p,p['revision'])
            self.update(j,asset_id=a['id'],status='done',ended_at=now(),message='New take imported unselected. Review identity, motion and timing before use.')
    def run(self):
        while not self.stop.is_set():
            try: jid=self.queue.get(timeout=.3)
            except queue.Empty: continue
            j=self.jobs[jid]
            try: self.process(j)
            except Exception as e:
                if isinstance(e,(ValueError,RuntimeError)):
                    message=str(e)
                else: message='The provider operation or local import was interrupted. Existing results are retained; no automatic resubmission.'
                status='recoverable' if j.get('remote_id') or j.get('result_file') else 'unknown' if j['status']=='submitting' else 'blocked'
                self.update(j,status=status,message=message,ended_at=now())
            finally: self.queue.task_done()
