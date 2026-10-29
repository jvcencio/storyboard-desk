'use strict';
let cloudProjectId='', cloudData=null, cloudQuote=null, cloudLoading=false;
const cloudMoney=n=>'$'+Number(n||0).toFixed(2);
function cloudDraft(){try{return JSON.parse(localStorage.getItem('sbd-cloud-'+projectId)||'{}');}catch(_){return {};}}
function saveCloudDraft(){
  const form=$('#cloud-form');if(!form||!cloudProjectId)return;
  localStorage.setItem('sbd-cloud-'+cloudProjectId,JSON.stringify({shot:$('#cloud-shot').value,kind:$('#cloud-kind').value,model:$('#cloud-model').value,prompt:$('#cloud-prompt').value,reference_ids:$$('#cloud-refs input:checked').map(x=>x.value),duration:Number($('#cloud-duration').value),resolution:$('#cloud-resolution').value}));
}
async function loadCloud(force=false){
  if(cloudLoading)return;cloudLoading=true;const pid=projectId;
  try{const r=await fetch('/api/cloud?project='+encodeURIComponent(pid),{cache:'no-store'});if(!r.ok)throw Error('Generation service unavailable. Reload after the SBD update.');const data=await r.json();if(pid!==projectId)return;cloudData=data;
    const changed=cloudProjectId!==pid;cloudProjectId=pid;
    if(force||changed){cloudQuote=null;renderCloudForm();}
    renderCloudStatus();
  }finally{cloudLoading=false;}
}
function syncCloudProject(){if(currentTab==='cloud'&&cloudProjectId!==projectId)safe(()=>loadCloud(true))();}
function renderCloudForm(){
  const p=project(),d=cloudDraft();
  $('#cloud-shot').innerHTML=p.shots.map((s,i)=>`<option value="${s.id}">${String((i+1)*10).padStart(3,'0')} · ${esc(s.title)} · ${esc(s.scene)}</option>`).join('');
  if(p.shots.some(s=>s.id===d.shot))$('#cloud-shot').value=d.shot;
  $('#cloud-kind').value=d.kind||'image';renderCloudModels(d.model);
  $('#cloud-duration').value=d.duration||8;$('#cloud-resolution').value=d.resolution||'720p';
  const refs=drawingImages().concat(p.shots.flatMap(s=>s.assets.filter(a=>a.kind==='clip'&&a.poster_path&&!a.source_ref&&!a.binned_at).map(a=>({id:'poster-'+a.id,path:a.poster_path,label:s.title+' · clip '+a.take+' poster (layout reference)'}))));
  $('#cloud-refs').innerHTML=refs.map(a=>`<label class="cloud-ref"><input type="checkbox" value="${a.id}" ${(d.reference_ids||[]).includes(a.id)?'checked':''}><img src="${media(a)}" alt=""><span>${esc(a.label)}</span></label>`).join('')||'<p class="fine">Add images through a shot or References to guide the new frame.</p>';
  $('#cloud-prompt').value=d.prompt||cloudDefaultPrompt();renderCloudQuote();
}
function cloudDefaultPrompt(){const s=project().shots.find(s=>s.id===$('#cloud-shot').value),p=project();const planned=cloudData?.brief?.content.shots.find(r=>r.shot_id===s.id);if(planned)return $('#cloud-kind').value==='video'?planned.video_prompt:planned.image_prompt;return $('#cloud-kind').value==='video'?`Animate the approved first frame. ${s.action}\nCAMERA: ${s.camera}\nENDING: ${s.transition}\nCONTINUITY: ${s.continuity}\nSilent picture only. No speech, music or effects.`:`Create one finished starting frame, ${p.aspect}. ${s.composition}\nLOOK: ${p.look}\nCONTINUITY: ${s.continuity}\n${s.reference_notes}\nUse the chosen references for identity and style. One scene, no collage, no reference-sheet layout, no captions.`;}
function renderCloudModels(preferred){
  if(!cloudData)return;const image=$('#cloud-kind').value==='image';
  const models=image?cloudData.image_models:cloudData.video_models;
  $('#cloud-model').innerHTML=models.map(m=>`<option value="${esc(m.id)}">${esc(m.name)}</option>`).join('');
  $('#cloud-model').value=models.some(m=>m.id===preferred)?preferred:image?'google/gemini-3-pro-image':'google/veo-3.1-fast';
  $('#cloud-reference-section').hidden=!image;$('#cloud-video-options').hidden=image;
  $('#cloud-starter-note').hidden=image;const s=project().shots.find(s=>s.id===$('#cloud-shot').value);
  $('#cloud-starter-note').textContent=s?.animation_ready?'Uses this shot’s selected, approved finished image. Audio generation is off.':'This shot needs an approved finished image. Create or import one, then approve it in Finished image.';
}
function renderCloudStatus(){
  if(!cloudData)return;const d=cloudData;
  $('#cloud-connection').textContent=d.connection_message;
  $('#cloud-ledger').textContent=`${cloudMoney(d.committed_usd)} charged or reserved / ${cloudMoney(d.allowance_usd)} total project allowance`;
  if(document.activeElement!==$('#cloud-allowance'))$('#cloud-allowance').value=d.allowance_usd;
  $('#cloud-jobs').innerHTML=d.jobs.map(j=>`<article class="cloud-job"><div><b>${esc(project().shots.find(s=>s.id===j.shot_id)?.title||j.shot_id)} · ${j.kind==='image'?'Image':'Video'}</b><p>${esc(j.status)} · ${esc(j.message)}</p><small>${esc(j.model)} · ${j.actual_cost_usd!=null?cloudMoney(j.actual_cost_usd)+' reported':cloudMoney(j.reserved_usd)+' reserved'}</small></div><div>${j.asset_id?`<button class="secondary" data-cloud-open="${j.id}">Review take</button>`:''}${['recoverable','unknown','blocked'].includes(j.status)&&(j.remote_id||j.result_file)?`<button class="quiet" data-cloud-resume="${j.id}">Check existing job</button>`:''}</div><details><summary>Prompt and inputs</summary><p class="cloud-pre">${esc(j.prompt)}</p><small>${j.refs.map(r=>esc(r.title)).join(' · ')}</small></details></article>`).join('')||'<p class="fine">No cloud jobs yet. Prepare a request to review its inputs and estimated cost.</p>';
  const b=d.brief;
  $('#cloud-brief').hidden=!b;
  if(b){const c=b.content;$('#cloud-brief-title').textContent=c.title;$('#cloud-brief-summary').textContent=c.summary;
    $('#cloud-brief-state').textContent=b.approved_sha256===b.sha256?'Production brief approved':'Production brief ready for your review';
    $('#cloud-brief-body').innerHTML=`<p class="cloud-pre">${esc(c.direction)}</p><p class="fine">${esc(c.timing_note)}</p><h3>First qualification batch</h3><p>${esc(c.first_batch.scope)}</p><p>Proposed allowance: ${cloudMoney(c.first_batch.proposed_total_allowance_usd)}. Approving the brief does not set an allowance or submit a request.</p><h3>Generation and review</h3><ol>${c.review_sequence.map(v=>`<li>${esc(v)}</li>`).join('')}</ol>`+c.shots.map((s,i)=>`<article class="cloud-plan-row"><div><b>${esc(s.title)}</b><p>${esc(s.purpose)}</p><small>${esc(s.acceptance)}</small><details><summary>Starting image and video directions</summary><p class="cloud-pre">${esc(s.image_prompt)}</p><p class="cloud-pre">${esc(s.video_prompt)}</p><p class="fine">${s.proposed_takes} takes proposed for a later production batch${s.optional?' · optional alternative':''}.</p></details></div><button class="quiet" data-cloud-plan="${i}">Prepare image ↗</button></article>`).join('');
    $('#cloud-approve-brief').hidden=b.approved_sha256===b.sha256;
  }
  renderCloudQuote();
}
function renderCloudQuote(){
  const q=cloudQuote;$('#cloud-quote').hidden=!q;if(!q)return;
  const b=cloudData?.brief,briefOkay=!q.brief_sha256||b?.approved_sha256===q.brief_sha256;
  const funds=cloudData&&cloudData.committed_usd+q.reserved_usd<=cloudData.allowance_usd+0.00001;
  $('#cloud-quote-content').innerHTML=`<b>${esc(q.model)} · ${q.resolution}${q.duration?' · '+q.duration+'s · audio off':''} · ${esc(q.aspect)}</b><p>${esc(q.price_note)}</p><p><b>${cloudMoney(q.reserved_usd)} will be reserved on submission</b></p><p class="fine">Sends this prompt and ${q.refs.length} image(s) to OpenRouter and Google: ${q.refs.map(r=>esc(r.title)).join(' · ')||'no input images'}.</p><div class="cloud-quote-refs">${q.refs.map(r=>`<figure><img src="${media(r)}" alt="${esc(r.title)}"><figcaption>${esc(r.title)}</figcaption></figure>`).join('')}</div><details><summary>Exact generation prompt</summary><p class="cloud-pre">${esc(q.prompt)}</p></details><p class="fine">${!briefOkay?'Approve the production brief above first. ':''}${!funds?'Set enough project allowance above first. ':''}${cloudData?.connected!==true?'Check the saved OpenRouter connection first.':''}</p>`;
  $('#cloud-generate').textContent=q.kind==='image'?'Generate image':'Generate silent Veo take';
  $('#cloud-generate').disabled=!briefOkay||!funds||cloudData?.connected!==true;
}
function openCloudForShot(){
  const sid=shotId,kind=stage==='clip'?'video':'image';
  const d=cloudDraft();d.shot=sid;d.kind=kind;d.model=kind==='image'?'google/gemini-3-pro-image':'google/veo-3.1-fast';d.prompt='';
  localStorage.setItem('sbd-cloud-'+projectId,JSON.stringify(d));$('#shot-dialog').close();switchTab('cloud');
}
document.addEventListener('DOMContentLoaded',()=>{
  $('#cloud-form').addEventListener('submit',safe(async e=>{e.preventDefault();saveCloudDraft();const d=cloudDraft();const r=await api('cloud-prepare',{...d,project:projectId,revision:project().revision});cloudQuote=r.quote;renderCloudQuote();toast('Request prepared. No generation submitted.');}));
  $('#cloud-form').addEventListener('input',()=>{cloudQuote=null;saveCloudDraft();renderCloudQuote();});
  $('#cloud-kind').addEventListener('change',()=>{renderCloudModels();$('#cloud-prompt').value=cloudDefaultPrompt();saveCloudDraft();});
  $('#cloud-shot').addEventListener('change',()=>{renderCloudModels($('#cloud-model').value);$('#cloud-prompt').value=cloudDefaultPrompt();saveCloudDraft();});
  document.addEventListener('click',safe(async e=>{
    const b=e.target.closest('button');if(!b)return;
    if(b.id==='open-cloud-shot'){if(dirty)await saveShot();openCloudForShot();return;}
    if(b.id==='cloud-connect'){b.disabled=true;try{cloudData=await api('cloud-connect',{project:projectId});renderCloudStatus();}finally{b.disabled=false;}return;}
    if(b.id==='cloud-save-allowance'){cloudData=await api('cloud-allowance',{project:projectId,allowance_usd:Number($('#cloud-allowance').value)});renderCloudStatus();toast('Project allowance saved. No generation submitted.');}
    if(b.id==='cloud-approve-brief'){cloudData=await api('cloud-approve-brief',{project:projectId,sha256:cloudData.brief.sha256});renderCloudStatus();toast('Production brief approved. No generation submitted.');}
    if(b.id==='cloud-generate'){if(!cloudQuote)return;b.disabled=true;try{await api('cloud-submit',{quote:cloudQuote.id,confirmed:true});cloudQuote=null;await loadCloud();toast('Generation queued. The new take returns here for review.');}finally{renderCloudQuote();}}
    if(b.dataset.cloudResume){await api('cloud-resume',{job:b.dataset.cloudResume});await loadCloud();}
    if(b.dataset.cloudOpen){const j=cloudData.jobs.find(j=>j.id===b.dataset.cloudOpen);await refresh();await openShot(j.shot_id);stage=j.kind==='image'?'keyframe':'clip';previewId=j.asset_id;await renderStage();}
    if(b.dataset.cloudPlan!=null){const row=cloudData.brief.content.shots[Number(b.dataset.cloudPlan)];$('#cloud-shot').value=row.shot_id;$('#cloud-kind').value='image';renderCloudModels();$('#cloud-prompt').value=row.image_prompt;$$('#cloud-refs input').forEach(x=>x.checked=row.reference_ids.includes(x.value));cloudQuote=null;saveCloudDraft();renderCloudQuote();$('#cloud-form').scrollIntoView({block:'start',behavior:'smooth'});}
  }));
  setInterval(()=>{if(currentTab==='cloud')safe(()=>loadCloud())();},8000);
});
