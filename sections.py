"""Song-anchored clip assemblies and editor handoffs. No model calls."""
import copy, csv, datetime, hashlib, io, json, math, secrets, shutil, subprocess, tempfile, wave, zipfile
from fractions import Fraction
from pathlib import Path
from urllib.parse import quote
import xml.etree.ElementTree as ET

APP=Path(__file__).resolve().parent
KINDS=('none','verse','pre-chorus','chorus','bridge','intro','outro','instrumental','other')
def ident():return secrets.token_hex(6)
def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def number(value):
    if isinstance(value,bool):raise ValueError('Enter a time in seconds.')
    try:value=float(value)
    except (TypeError,ValueError):raise ValueError('Enter a valid time.') from None
    if not math.isfinite(value):raise ValueError('Time must be finite.')
    return round(value,6)
def label(value):
    if not isinstance(value,str) or not 0<len(value.strip())<=100:raise ValueError('Give the section a name of 1–100 characters.')
    return value.strip()
def rational(value,rate=24):
    f=Fraction(round(number(value)*rate),rate)
    return str(f.numerator)+'s' if f.denominator==1 else f'{f.numerator}/{f.denominator}s'
def hash_value(v):return hashlib.sha256(json.dumps(v,sort_keys=True).encode()).hexdigest()

def fcpxml(project,section,entries,source_names,options=()):
    """Portable source links; section-local editing clock, original-song audio in-point."""
    root=ET.Element('fcpxml',version='1.10');resources=ET.SubElement(root,'resources')
    width,height={'16:9':('1280','720'),'9:16':('720','1280'),'1:1':('1080','1080')}[project['aspect']]
    ET.SubElement(resources,'format',id='r1',name='SBD 24 fps',frameDuration='1/24s',width=width,height=height,colorSpace='1-1-1')
    refs={}
    for entry in list(entries)+list(options):
        a=entry['asset'];key=(entry['source_shot'],a['id'])
        if key in refs:continue
        rid='r'+str(len(refs)+2);refs[key]=rid
        node=ET.SubElement(resources,'asset',id=rid,name=entry['title'],start='0s',duration=rational(a['duration_seconds']),hasVideo='1',hasAudio='1' if a.get('audio_tracks',1) else '0',format='r1')
        ET.SubElement(node,'media-rep',kind='original-media',src=quote(source_names[key],safe='/'))
    track=project['soundtrack'];audio=ET.SubElement(resources,'asset',id='music',name=track.get('original_name','Original song'),start='0s',duration=rational(track['duration_seconds']),hasVideo='0',hasAudio='1',audioSources='1',audioChannels=str(track.get('channels',2)),audioRate=str(track.get('sample_rate',48000)))
    ET.SubElement(audio,'media-rep',kind='original-media',src='soundtrack/original.wav')
    library=ET.SubElement(root,'library');event=ET.SubElement(library,'event',name=project['title']);proj=ET.SubElement(event,'project',name=section['label']+' · SBD assembly')
    length=max(section['end_seconds']-section['start_seconds'],sum(e['out_seconds']-e['in_seconds'] for e in entries))
    for e in options:
        browser_clip=ET.SubElement(event,'asset-clip',name=e['title']+' · Coverage option',ref=refs[(e['source_shot'],e['asset']['id'])],start='0s',duration=rational(e['asset']['duration_seconds']),srcEnable='video')
        ET.SubElement(browser_clip,'keyword',start='0s',duration=rational(e['asset']['duration_seconds']),value=section['label']+' · Coverage options')
    seq=ET.SubElement(proj,'sequence',format='r1',duration=rational(length),tcStart='0s',tcFormat='NDF',audioLayout='stereo',audioRate='48k')
    spine=ET.SubElement(seq,'spine');gap=ET.SubElement(spine,'gap',name=section['label'],offset='0s',start='0s',duration=rational(length));elapsed=0
    for e in entries:
        ET.SubElement(gap,'asset-clip',name=e['title'],ref=refs[(e['source_shot'],e['asset']['id'])],lane='1',offset=rational(elapsed),start=rational(e['in_seconds']),duration=rational(e['out_seconds']-e['in_seconds']),srcEnable='video')
        elapsed+=e['out_seconds']-e['in_seconds']
    ET.SubElement(gap,'asset-clip',name=track.get('original_name','Original song'),ref='music',lane='-1',offset='0s',start=rational(section['start_seconds']),duration=rational(section['end_seconds']-section['start_seconds']),srcEnable='audio',audioRole='music')
    for c in project.get('music_cues',[]):
        if c['track_sha256']!=section['track_sha256']:continue
        start=max(section['start_seconds'],c['seconds']);end=min(section['end_seconds'],c.get('end_seconds',c['seconds']))
        if section['start_seconds']<=c['seconds']<section['end_seconds'] or end>start:
            duration=max(1/24,end-start)
            ET.SubElement(gap,'marker',start=rational(start-section['start_seconds']),duration=rational(duration),value=c['label'],note=f"Original song: {c['seconds']}–{c.get('end_seconds',c['seconds'])} seconds")
    ET.SubElement(gap,'marker',start=rational(section['end_seconds']-section['start_seconds']-1/24),duration='1/24s',value=section['label']+' ends',note='Owner section boundary; assembly may have extra coverage beyond this point.')
    ET.indent(root)
    return '<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE fcpxml>\n'+ET.tostring(root,encoding='unicode')+'\n'

# Shared media adapter preserves the native Mac path and adds FFmpeg.
from media_tools import render_range, render_reel

class SectionStore:
    @staticmethod
    def coverage_key(row):
        return 'sha:'+row['source_sha256'] if row.get('source_sha256') else 'asset:'+row['source_shot']+'/'+row['source_asset']
    def asset_key(self,p,asset):
        ref=asset.get('source_ref',{})
        digest=asset.get('sha256') or ref.get('sha256')
        if digest:return 'sha:'+digest
        if ref:return 'asset:'+ref['shot_id']+'/'+ref['asset_id']
        owner=next((s['id'] for s in p['shots'] if any(a['id']==asset['id'] for a in s['assets'])),None)
        return 'asset:'+str(owner)+'/'+asset['id']
    def prepare_coverage(self,p):
        # Reading legacy sections exposes a stable bin; persistence occurs only on save.
        for section in p.get('music_sections',[]):
            section.setdefault('story_scene',section['label'])
            coverage=section.setdefault('coverage',[]);seen={self.coverage_key(c) for c in coverage}
            for entry in section['clips']:
                key=self.coverage_key(entry)
                if key in seen:continue
                seen.add(key);coverage.append(dict(entry,id='cov-'+hashlib.sha256(key.encode()).hexdigest()[:12],in_seconds=0,out_seconds=entry['source_duration_seconds'],angle='',note='',exclusive=False,include_in_handoff=True))
    def reservation_conflicts(self,p,owner_id,key):
        owner=self.section(p,owner_id);conflicts=[]
        for section in p.get('music_sections',[]):
            if section['id']==owner_id:continue
            if any(self.coverage_key(c)==key for c in section.get('coverage',[])+section['clips']):conflicts.append('section '+section['label'])
        for shot in p['shots']:
            chosen=next((a for a in shot['assets'] if a['id']==shot.get('selected_clip')),None)
            if chosen and self.asset_key(p,chosen)==key and shot.get('scene','').casefold()!=owner.get('story_scene',owner['label']).casefold():conflicts.append('shot '+shot['title']+' ('+(shot.get('scene') or 'ungrouped')+')')
        return conflicts
    def validate_reservations(self,p):
        self.prepare_coverage(p);owners={}
        for section in p.get('music_sections',[]):
            for row in section.get('coverage',[]):
                if not row.get('exclusive'):continue
                key=self.coverage_key(row)
                if key in owners and owners[key]['id']!=section['id']:raise ValueError('This source is already reserved for '+owners[key]['label']+'. Release that reservation first.')
                owners[key]=section
        for key,section in owners.items():
            conflicts=self.reservation_conflicts(p,section['id'],key)
            if conflicts:raise ValueError('Reserved for '+section['label']+'. Conflicting use: '+', '.join(conflicts)+'. Release the reservation or remove the conflicting use first.')
    def coverage_add(self,pid,revision,sid,raw):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before collecting footage.')
            section=self.section(p,sid);self.section_track(p,section)
            source_sid,a,_=self.section_source(p,raw['source_shot'],raw['source_asset']);st,en,duration=self.section_range(a,raw['in_seconds'],raw['out_seconds'],raw.get('duration_seconds'))
            e={'source_shot':source_sid,'source_asset':a['id'],'source_sha256':a.get('sha256'),'source_duration_seconds':duration};key=self.coverage_key(e)
            existing=next((row for row in section['coverage'] if self.coverage_key(row)==key),None)
            if existing:return {'project':p,'section_id':sid,'coverage_id':existing['id'],'already_collected':True}
            if len(section['coverage'])>=300:raise ValueError('Use at most 300 source options per section.')
            angle=raw.get('angle','');note=raw.get('note','')
            music_tag=raw.get('music_tag','none')
            if music_tag not in KINDS:raise ValueError('Choose a valid musical tag.')
            if not isinstance(angle,str) or len(angle)>100 or not isinstance(note,str) or len(note)>2000:raise ValueError('Coverage label or note is too long.')
            self.section_history(p);e.update(id='cov-'+hashlib.sha256(key.encode()).hexdigest()[:12],in_seconds=st,out_seconds=en,angle=angle,note=note,music_tag=music_tag,exclusive=False,include_in_handoff=True,added_at=now());section['coverage'].append(e)
            return {'project':self.save(p,revision),'section_id':sid,'coverage_id':e['id'],'already_collected':False}
    def coverage_edit(self,pid,revision,sid,raw):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before editing coverage.')
            section=self.section(p,sid);row=next((c for c in section['coverage'] if c['id']==raw.get('coverage')),None)
            if not row:raise ValueError('Choose a source option in this section.')
            self.section_history(p)
            if raw.get('operation')=='remove':
                if any(self.coverage_key(e)==self.coverage_key(row) for e in section['clips']):raise ValueError('Remove this source’s assembly ranges first. Other coverage options remain available.')
                section['coverage'].remove(row)
            elif raw.get('operation')=='move':
                index=section['coverage'].index(row);delta=raw.get('delta')
                if isinstance(delta,bool) or delta not in (-1,1) or not 0<=index+delta<len(section['coverage']):raise ValueError('Choose a valid earlier/later reel position.')
                section['coverage'][index],section['coverage'][index+delta]=section['coverage'][index+delta],row
            elif raw.get('operation')=='update':
                if 'music_tag' in raw:
                    if raw['music_tag'] not in KINDS:raise ValueError('Choose a valid musical tag.')
                    row['music_tag']=raw['music_tag']
                for key in ('exclusive','include_in_handoff'):
                    if key in raw:
                        if not isinstance(raw[key],bool):raise ValueError('Use a checkbox for reservation and handoff options.')
                        if key=='exclusive' and raw[key]:self.section_track(p,section)
                        row[key]=raw[key]
                for key,limit in (('angle',100),('note',2000)):
                    if key in raw:
                        if not isinstance(raw[key],str) or len(raw[key])>limit:raise ValueError('Coverage label or note is too long.')
                        row[key]=raw[key]
                if 'in_seconds' in raw or 'out_seconds' in raw:
                    _,a,_=self.section_source(p,row['source_shot'],row['source_asset']);a=dict(a,duration_seconds=a.get('duration_seconds',row['source_duration_seconds']));st,en,_=self.section_range(a,raw.get('in_seconds',row['in_seconds']),raw.get('out_seconds',row['out_seconds']));row.update(in_seconds=st,out_seconds=en)
            else:raise ValueError('Unknown coverage edit.')
            return {'project':self.save(p,revision),'section_id':sid}
    def section(self,p,sid):
        row=next((s for s in p.get('music_sections',[]) if s['id']==sid),None)
        if not row:raise ValueError('Choose an existing song section.')
        return row
    def section_track(self,p,section=None):
        track=p.get('soundtrack')
        if not p.get('music_video') or not track:raise ValueError('Attach the original song before creating sections.')
        if section and section['track_sha256']!=track['sha256']:raise ValueError('This section belongs to an older soundtrack. Create a section for the current recording.')
        return track
    def section_source(self,p,sid,aid):
        a=next((a for a in self.shot(p,sid)['assets'] if a['id']==aid and a['kind']=='clip' and not a.get('binned_at')),None)
        if not a:raise ValueError('Choose an active clip in this project.')
        if a.get('source_ref'):return self.section_source(p,a['source_ref']['shot_id'],a['source_ref']['asset_id'])
        root=self.folder(p['id']).resolve();path=(root/a['path']).resolve()
        if not path.is_relative_to(root) or not a['path'].startswith('media/') or not path.is_file():raise ValueError('The source clip is unavailable.')
        if a.get('sha256') and hashlib.sha256(path.read_bytes()).hexdigest()!=a['sha256']:raise ValueError('Source bytes changed. Restore or re-import the clip.')
        return sid,a,path
    def section_range(self,asset,start,end,measured=None):
        start,end=number(start),number(end);duration=number(asset.get('duration_seconds',measured))
        if not 0<=start<end<=duration<=7200:raise ValueError('Choose an In/Out range within the source clip.')
        return start,end,duration
    def section_history(self,p):
        p.setdefault('section_history',[]).append({'at':now(),'before':copy.deepcopy(p.get('music_sections',[]))})
        p['section_history']=p['section_history'][-50:]
    def save_section(self,pid,revision,raw):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before saving the section.')
            track=self.section_track(p);old=self.section(p,raw['id']) if raw.get('id') else None
            if old:self.section_track(p,old)
            name=label(raw.get('label'));start,end=number(raw.get('start_seconds')),number(raw.get('end_seconds'));kind=raw.get('kind','other')
            if not 0<=start<end<=track['duration_seconds'] or kind not in KINDS:raise ValueError('Choose a valid section type and a time range within the song.')
            if any(s['label'].casefold()==name.casefold() and s['id']!=(old or {}).get('id') and s['track_sha256']==track['sha256'] for s in p.get('music_sections',[])):raise ValueError('Use a distinct name for this section.')
            note=raw.get('note','')
            if not isinstance(note,str) or len(note)>2000:raise ValueError('Section note is too long.')
            row=copy.deepcopy(old) if old else {'id':ident(),'track_sha256':track['sha256'],'clips':[],'created_at':now()}
            row.update(label=name,kind=kind,start_seconds=start,end_seconds=end,note=note,updated_at=now())
            if 'reel_scope' in raw:
                if raw['reel_scope'] not in ('coverage','preferred','assembly'):raise ValueError('Choose a valid default reel scope.')
                row['reel_scope']=raw['reel_scope']
            if 'clips' in raw:
                if not old or not isinstance(raw['clips'],list) or len(raw['clips'])!=len(old['clips']) or {e['id'] for e in raw['clips']}!={e['id'] for e in old['clips']}:raise ValueError('Refresh the section clip list before editing ranges.')
                cleaned=[]
                for r in raw['clips']:
                    e=copy.deepcopy(next(e for e in old['clips'] if e['id']==r['id']));_,a,_=self.section_source(p,e['source_shot'],e['source_asset']);a=dict(a,duration_seconds=a.get('duration_seconds',e['source_duration_seconds']));st,en,_=self.section_range(a,r['in_seconds'],r['out_seconds']);e.update(in_seconds=st,out_seconds=en);cleaned.append(e)
                row['clips']=cleaned
            self.section_history(p);p['music_sections']=[row if s['id']==row['id'] else s for s in p.get('music_sections',[])]
            if not old:p['music_sections'].append(row)
            return {'project':self.save(p,revision),'section_id':row['id']}
    def split_sections(self,pid,revision,raw):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before splitting this range.')
            track=self.section_track(p);start=number(raw.get('start_seconds'));end=number(raw.get('end_seconds'));span=number(raw.get('part_seconds'));prefix=label(raw.get('prefix','Part'))
            if not 0<=start<end<=track['duration_seconds'] or span<=0 or math.ceil((end-start)/span)>100:raise ValueError('Choose a range within the song and a size producing at most 100 portions.')
            kind=raw.get('kind','none')
            if kind not in KINDS:raise ValueError('Choose a valid musical tag.')
            for s in p.get('music_sections',[]):
                if s['track_sha256']==track['sha256'] and max(start,s['start_seconds'])<min(end,s['end_seconds']):raise ValueError('This range overlaps '+s['label']+'. Choose unused time or edit the existing section.')
            rows=[];cursor=start;batch=ident()
            while cursor<end:
                stop=min(end,round(start+(len(rows)+1)*span,6));name=label(prefix+' '+str(len(rows)+1).zfill(2))
                if any(s['label'].casefold()==name.casefold() and s['track_sha256']==track['sha256'] for s in p.get('music_sections',[])):raise ValueError('A section already uses '+name+'. Choose a different prefix.')
                if stop<=cursor:raise ValueError('Choose a larger portion size.')
                rows.append({'id':ident(),'label':name,'kind':kind,'start_seconds':cursor,'end_seconds':stop,'track_sha256':track['sha256'],'clips':[],'coverage':[],'note':'Fixed-time working portion. Musical labels can be added later; this boundary is not an inferred verse or beat.','created_at':now(),'updated_at':now(),'split_batch':batch,'reel_scope':'preferred'})
                cursor=stop
            self.section_history(p);p['music_sections']=sorted(p.get('music_sections',[])+rows,key=lambda s:s['start_seconds'])
            return {'project':self.save(p,revision),'section_ids':[s['id'] for s in rows]}
    def section_edit(self,pid,revision,sid,raw):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before editing section clips.')
            s=self.section(p,sid);self.section_track(p,s);operation=raw.get('operation');clips=s['clips'];self.section_history(p)
            if operation=='add':
                if len(clips)>=200:raise ValueError('Use at most 200 clip ranges per section.')
                source_sid,a,_=self.section_source(p,raw['source_shot'],raw['source_asset']);st,en,duration=self.section_range(a,raw['in_seconds'],raw['out_seconds'],raw.get('duration_seconds'))
                e={'id':ident(),'source_shot':source_sid,'source_asset':a['id'],'source_sha256':a.get('sha256'),'source_duration_seconds':duration,'in_seconds':st,'out_seconds':en,'added_at':now()}
                anchor=raw.get('anchor');position=raw.get('position','end')
                if position not in ('before','after','end'):raise ValueError('Choose before, after or end.')
                if position!='end':
                    index=next((i for i,v in enumerate(clips) if v['id']==anchor),None)
                    if index is None:raise ValueError('The insertion target changed. Choose it again.')
                    clips.insert(index+(position=='after'),e)
                else:clips.append(e)
            elif operation in ('remove','move'):
                index=next((i for i,v in enumerate(clips) if v['id']==raw.get('entry')),None)
                if index is None:raise ValueError('Choose a range in this section.')
                if operation=='remove':clips.pop(index)
                else:
                    delta=raw.get('delta')
                    if isinstance(delta,bool) or delta not in (-1,1) or not 0<=index+delta<len(clips):raise ValueError('Choose a valid earlier/later position.')
                    clips[index],clips[index+delta]=clips[index+delta],clips[index]
            else:raise ValueError('Unknown section edit.')
            s['updated_at']=now();return {'project':self.save(p,revision),'section_id':sid}
    def undo_section(self,pid,revision):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before undoing the section edit.')
            if not p.get('section_history'):raise ValueError('No section edit to undo.')
            previous=p['section_history'].pop();p['music_sections']=previous['before'];return {'project':self.save(p,revision)}
    def export_reel(self,pid,revision,sid,scope='coverage',renderer=render_reel):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before exporting the selects reel.')
            s=copy.deepcopy(self.section(p,sid));self.section_track(p,s);self.validate_reservations(p)
            if scope not in ('coverage','preferred','assembly'):raise ValueError('Choose full options, preferred ranges or assembly ranges.')
            rows=s['clips'] if scope=='assembly' else [c for c in s['coverage'] if c.get('include_in_handoff',True)]
            if not rows:raise ValueError('Choose footage to include in this selects reel.')
            segments=[];cuts=[];elapsed=0
            for i,e in enumerate(rows):
                ss,a,path=self.section_source(p,e['source_shot'],e['source_asset'])
                if e.get('source_sha256') and e['source_sha256']!=a.get('sha256'):raise ValueError('A selected source changed. Review the footage again.')
                a=dict(a,duration_seconds=a.get('duration_seconds',e['source_duration_seconds']))
                start,end,_=self.section_range(a,0 if scope=='coverage' else e['in_seconds'],a['duration_seconds'] if scope=='coverage' else e['out_seconds'])
                segments.append({'path':str(path),'start':start,'end':end})
                cuts.append({'order':i+1,'entry_id':e['id'],'title':self.shot(p,ss)['title'],'take':a['take'],'source_shot':ss,'source_asset':a['id'],'source_sha256':a.get('sha256'),'source_in_seconds':start,'source_out_seconds':end,'reel_in_seconds':round(elapsed,6),'reel_out_seconds':round(elapsed+end-start,6),'angle':e.get('angle',''),'note':e.get('note',''),'music_tag':e.get('music_tag','none'),'exclusive':e.get('exclusive',False)})
                elapsed+=end-start
            root=self.folder(pid).resolve()
        exports=root/'exports';exports.mkdir(exist_ok=True);stem='selects-'+ident();destination=exports/(stem+'.mp4');files=[]
        try:
            with tempfile.TemporaryDirectory(prefix=stem+'-',dir=exports) as tmp:
                tmp=Path(tmp);result=renderer(segments,tmp/'reel.mp4',p['aspect'])
                expected=round(elapsed,6)
                if result.get('audio_tracks')!=0 or abs(result.get('duration_seconds',0)-expected)>.09:raise ValueError('The selects reel failed its silent-picture/duration check.')
                manifest={'schema':'sbd-selects-reel/1','project_id':pid,'project_revision':revision,'section_id':sid,'section_label':s['label'],'song_start_seconds':s['start_seconds'],'song_end_seconds':s['end_seconds'],'song_section_seconds':s['end_seconds']-s['start_seconds'],'reel_seconds':expected,'scope':scope,'cuts':cuts,'export_metadata':result,'exported_at':now(),'editing_note':'Overlength silent picture reel. Split, trim and rearrange in your editor. Song timing is a target only; no footage is stretched or cut to fit. Audio remains separate.'}
                (tmp/'cuts.json').write_text(json.dumps(manifest,indent=2,ensure_ascii=False)+'\n', encoding='utf-8')
                with (tmp/'cuts.csv').open('w',newline='', encoding='utf-8') as out:
                    writer=csv.DictWriter(out,fieldnames=list(cuts[0]));writer.writeheader();writer.writerows(cuts)
                for source,suffix in (('reel.mp4','.mp4'),('cuts.json','.json'),('cuts.csv','.csv')):
                    dest=exports/(stem+suffix);(tmp/source).replace(dest);files.append(dest)
        except BaseException:
            for file in files:file.unlink(missing_ok=True)
            raise
        return {'url':'/media/'+pid+'/exports/'+destination.name,'map_url':'/media/'+pid+'/exports/'+stem+'.json','csv_url':'/media/'+pid+'/exports/'+stem+'.csv','duration_seconds':expected,'target_seconds':s['end_seconds']-s['start_seconds']}
    def export_section(self,pid,revision,sid,renderer=render_range,include_coverage=False):
        with self.lock:
            p=self.load(pid)
            if p['revision']!=revision:raise RuntimeError('Project changed. Reload before exporting this section.')
            s=copy.deepcopy(self.section(p,sid));self.section_track(p,s);self.validate_reservations(p)
            if not isinstance(include_coverage,bool):raise ValueError('Choose whether to include coverage options.')
            included=[c for c in s.get('coverage',[]) if c.get('include_in_handoff',True)] if include_coverage else []
            if not s['clips'] and not included:raise ValueError('Add assembly ranges or coverage options before exporting.')
            entries=[];options=[]
            for e in s['clips']:
                ss,a,path=self.section_source(p,e['source_shot'],e['source_asset'])
                if e.get('source_sha256') and e['source_sha256']!=a.get('sha256'):raise ValueError('A tagged source changed; review the section again.')
                a=dict(a,duration_seconds=a.get('duration_seconds',e['source_duration_seconds']));self.section_range(a,e['in_seconds'],e['out_seconds'])
                entries.append(dict(e,source_shot=ss,asset=copy.deepcopy(a),path=path,title=self.shot(p,ss)['title']+' · Take '+str(a['take'])))
            for e in included:
                ss,a,path=self.section_source(p,e['source_shot'],e['source_asset'])
                if e.get('source_sha256') and e['source_sha256']!=a.get('sha256'):raise ValueError('A coverage source changed; review the bin again.')
                a=dict(a,duration_seconds=a.get('duration_seconds',e['source_duration_seconds']));self.section_range(a,e['in_seconds'],e['out_seconds'])
                options.append(dict(e,source_shot=ss,asset=copy.deepcopy(a),path=path,title=(e.get('angle') or self.shot(p,ss)['title'])+' · Take '+str(a['take'])))
            root=self.folder(pid).resolve();track_path=(root/p['soundtrack']['path']).resolve()
            if not track_path.is_relative_to(root) or not track_path.is_file():raise ValueError('Original soundtrack is unavailable.')
            if hashlib.sha256(track_path.read_bytes()).hexdigest()!=s['track_sha256']:raise ValueError('Original soundtrack bytes changed; export stopped.')
        exports=root/'exports';exports.mkdir(exist_ok=True);export_id=ident();destination=exports/('section-'+export_id+'.zip');partial=destination.with_suffix('.part')
        try:
            with tempfile.TemporaryDirectory(prefix='section-'+export_id+'-',dir=exports) as tmp:
                tmp=Path(tmp);(tmp/'mp4-ranges').mkdir();(tmp/'coverage-options').mkdir();range_rows=[];option_rows=[];source_names={};elapsed=0
                for i,e in enumerate(entries):
                    key=(e['source_shot'],e['asset']['id']);source_names[key]='sources/'+e['asset']['id']+e['path'].suffix.lower()
                    name=f"mp4-ranges/{i+1:03d}_{e['source_asset']}.mp4";meta=renderer(e['path'],tmp/name,e['in_seconds'],e['out_seconds'])
                    range_rows.append({'order':i+1,'entry_id':e['id'],'title':e['title'],'mp4':name,'source':source_names[key],'source_in_seconds':e['in_seconds'],'source_out_seconds':e['out_seconds'],'assembly_start_seconds':round(elapsed,6),'song_start_seconds':round(s['start_seconds']+elapsed,6),'export_metadata':meta});elapsed+=e['out_seconds']-e['in_seconds']
                for i,e in enumerate(options):
                    key=(e['source_shot'],e['asset']['id']);source_names[key]='sources/'+e['asset']['id']+e['path'].suffix.lower()
                    name=f"coverage-options/{i+1:03d}_{e['source_asset']}.mp4";meta=renderer(e['path'],tmp/name,0,e['asset']['duration_seconds'])
                    option_rows.append({'coverage_id':e['id'],'title':e['title'],'mp4':name,'source':source_names[key],'source_shot':e['source_shot'],'source_asset':e['source_asset'],'suggested_in_seconds':e['in_seconds'],'suggested_out_seconds':e['out_seconds'],'angle':e.get('angle',''),'note':e.get('note',''),'music_tag':e.get('music_tag','none'),'exclusive':e.get('exclusive',False),'export_metadata':meta})
                with wave.open(str(track_path),'rb') as src:
                    excerpt=io.BytesIO()
                    with wave.open(excerpt,'wb') as dst:
                        dst.setparams(src.getparams());src.setpos(round(s['start_seconds']*src.getframerate()));dst.writeframes(src.readframes(round((s['end_seconds']-s['start_seconds'])*src.getframerate())))
                cues=[c for c in p.get('music_cues',[]) if c['track_sha256']==s['track_sha256'] and c['seconds']<s['end_seconds'] and (c['end_seconds']>s['start_seconds'] if 'end_seconds' in c else c['seconds']>=s['start_seconds'])]
                manifest={'schema':'sbd-section-handoff/1','project_id':pid,'project_revision':revision,'section':s,'assembly_seconds':round(elapsed,6),'song_section_seconds':s['end_seconds']-s['start_seconds'],'range_files':range_rows,'coverage_options':option_rows,'include_coverage':include_coverage,'cues':cues,'clip_audio':'excluded from numbered MP4 ranges and FCPXML video clips; full sources retain original tracks','timeline_clock':'local 0 corresponds to original-song section start','frame_rate':24,'exported_at':now()}
                with zipfile.ZipFile(partial,'x',compression=zipfile.ZIP_STORED) as z:
                    z.writestr('section.json',json.dumps(manifest,indent=2,ensure_ascii=False));z.writestr('Final Cut.fcpxml',fcpxml(p,s,entries,source_names,options));z.writestr('soundtrack/section.wav',excerpt.getvalue());z.write(track_path,'soundtrack/original.wav')
                    for row in range_rows+option_rows:z.write(tmp/row['mp4'],row['mp4'])
                    z.writestr('coverage.json',json.dumps({'included':include_coverage,'options':option_rows,'all_candidates':s.get('coverage',[])},indent=2,ensure_ascii=False))
                    for key,name in source_names.items():z.write(next(e['path'] for e in entries+options if (e['source_shot'],e['asset']['id'])==key),name)
                    table=io.StringIO();w=csv.writer(table);w.writerow(['Order','Title','MP4 range','Full source','In seconds','Out seconds','Assembly start seconds','Original-song start seconds'])
                    for r in range_rows:w.writerow([r['order'],r['title'],r['mp4'],r['source'],r['source_in_seconds'],r['source_out_seconds'],r['assembly_start_seconds'],r['song_start_seconds']])
                    z.writestr('timing.csv',table.getvalue());cue_csv=io.StringIO();w=csv.writer(cue_csv);w.writerow(['Cue','Song start','Song end','Section-local start','Section-local end'])
                    for c in cues:w.writerow([c['label'],c['seconds'],c.get('end_seconds',''),c['seconds']-s['start_seconds'],c.get('end_seconds',c['seconds'])-s['start_seconds']])
                    z.writestr('lyric-cues.csv',cue_csv.getvalue());z.writestr('README.txt',f"{s['label']} · original song {s['start_seconds']}–{s['end_seconds']} seconds\n\nExtract this ZIP before importing.\n\nCoverage options: {len(option_rows)} full-length silent MP4s in coverage-options/ when included. They are alternatives to choose from, not extra timeline duration. coverage.json retains suggested source ranges, angle notes and reservation labels. Final Cut imports these as keyword-tagged browser clips, separate from the assembly project. You can cut considerably and choose a different angle without inserting every option. Reservations govern placements inside SBD; editing the exported files is under the editor’s control.\n\niMovie: import the numbered files in mp4-ranges/ and soundtrack/section.wav. They are separate silent MP4 ranges in assembly order; trimming, reordering and musical alignment are yours to refine. Full sources in sources/ retain edit handles.\nFinal Cut: import Final Cut.fcpxml. Its relative media links use sources/ and soundtrack/original.wav. The section-local timeline starts at 0, using the original song from {s['start_seconds']} seconds. Video clips remain separate and editable on a connected lane; only the original song supplies audio. A 24fps interchange clock quantizes boundaries to frames. Import has not been verified inside Final Cut. If media is offline, relink to the extracted sources/ and soundtrack/ folders.\n\nSection: {s['end_seconds']-s['start_seconds']:.3f}s; assembly: {elapsed:.3f}s. No clip was stretched or automatically fitted to the music. An overlong assembly remains extra coverage for the editor; a short assembly leaves space. This package contains individual MP4s, not a flattened movie.\n\nCue ranges can cross the section boundary. Full owner timings remain in section.json and lyric-cues.csv; XML markers are bounded to the section. Do not infer a different chorus end from a crossing lyric cue. Original project files and source media are untouched.\n")
            partial.replace(destination)
        finally:
            if partial.exists():partial.unlink()
        return 'exports/'+destination.name
