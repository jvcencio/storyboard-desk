'use strict';
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
let state, projectId, shotId, stage = 'sketch', previewId, dirty = false, promptText = '', reviewIndex = 0;
let currentTab = 'board', refreshing = false, toastTimer, sceneFilter = '', reviewList = [], reviewMode = 'clips', handoffPreviewBasis = '', submittingDraft = false;
const SHOT_FIELDS = ['title','purpose','composition','action','camera','sound','continuity','feedback','transition','scene','story_status','dialogue','narration','audience','jorge','anomaly','start_state','end_state','screen_direction','assets_needed','reference_notes','blocking','local_prompt','local_negative_prompt'];
const timecode = n => `${Math.floor(n/60)}:${String(Math.floor(n%60)).padStart(2,'0')}`;
const visibleShots = () => project().shots.filter(s=>!sceneFilter || s.scene===sceneFilter);
const handoffScope = () => {const value=$('#handoff-scene').value;return value.startsWith('shot:')?{shot:value.slice(5)}:{scene:value.startsWith('scene:')?value.slice(6):value||null};};
const storyLabel = s => s.story_status==='existing_candidate'?'Imported candidate':s.story_status==='reported_existing'?'Reported beat · media pending':s.story_status==='proposed'?'Pilot proposal':'Source unspecified';
const project = () => state.projects.find(p => p.id === projectId);
const shot = () => project()?.shots.find(s => s.id === shotId);
const media = a => `/media/${projectId}/${a.path.split('/').map(encodeURIComponent).join('/')}`;
const selected = (s, kind) => s.assets.find(a => a.id === s['selected_' + kind] && !a.binned_at);
const empty = label => `<div class="empty-art"><span class="frame-mark"></span><span>${esc(label)}</span></div>`;

function toast(message) { clearTimeout(toastTimer); $('#toast').textContent = message; $('#toast').hidden = false; toastTimer = setTimeout(() => $('#toast').hidden = true, 4200); }
function error(e) { $('#error').textContent = e.message || String(e); $('#error').hidden = false; toast(e.message || String(e)); }
async function api(route, body) {
  const response = await fetch('/api/' + route, {method:'POST', headers:{'Content-Type':'application/json','X-Storyboard-Token':state.token}, body:JSON.stringify(body)});
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || 'The action could not be completed.');
  $('#error').hidden = true;
  return data;
}
function accept(p) { state.projects = state.projects.map(old => old.id === p.id ? p : old); renderBoard(); }
function safe(fn) { return async (...args) => { try { await fn(...args); } catch (e) { error(e); } }; }
async function copyText(value) {
  try { await navigator.clipboard.writeText(value); }
  catch (_) { const input = document.createElement('textarea'); input.value = value; document.body.append(input); input.select(); if (!document.execCommand('copy')) throw new Error('Clipboard unavailable. Select and copy the prompt text.'); input.remove(); }
  toast('Copied to clipboard');
}
function download(name, value, type = 'text/plain') {
  const url = URL.createObjectURL(new Blob([value], {type}));
  const link = document.createElement('a'); link.href = url; link.download = name; link.click(); setTimeout(() => URL.revokeObjectURL(url), 10000);
}
function plan(p) { return {title:p.title, brief:p.brief, look:p.look, continuity:p.continuity, episode_notes:p.episode_notes, runtime_limit_seconds:p.runtime_limit_seconds, aspect:p.aspect, references:p.references, shots:p.shots.map(s => Object.fromEntries(['id',...SHOT_FIELDS,'duration'].map(k => [k,s[k]])))}; }
async function createProject(raw) {
  const r=await api('new',raw);state.projects.push(r.project);projectId=r.project.id;sceneFilter='';
  localStorage.setItem('storyboard-project',projectId);history.replaceState(null,'','?project='+encodeURIComponent(projectId));$('#new-dialog').close();renderBoard();fillDirection();switchTab('direction');toast('New storyboard created');
}

async function refresh(initial = false) {
  if (refreshing) return;
  refreshing = true;
  try {
    const r = await fetch('/api/state', {cache:'no-store'});
    if (!r.ok) throw new Error('Cannot reach the local workspace. Use Start Storyboard Desk.command.');
    const next = await r.json();
    if (initial) {
      state = next;
      projectId = new URLSearchParams(location.search).get('project') || localStorage.getItem('storyboard-project');
      if (!state.projects.some(p => p.id === projectId)) projectId = state.projects[0]?.id;
      renderBoard(); fillDirection();
    } else {
      // Never silently replace an editor's revision while its form contains local changes.
      const editorOpen = dirty || libraryDirty || sectionDirty || currentTab === 'direction' && $('#direction-form').dataset.dirty === 'yes';
      const current = project(), priorRevision = current?.revision;
      const completedDraft = next.jobs.some(j => j.project_id === projectId && j.shot_id === shotId && j.status === 'done' && state.jobs.find(old => old.id === j.id)?.status !== 'done');
      state = next;
      if (editorOpen && current) state.projects = next.projects.map(p => p.id === current.id ? current : p);
      renderBoard();
      if ($('#shot-dialog').open && !dirty && project()?.revision !== priorRevision) {
        if (stage === 'sketch' && completedDraft) previewId = shot()?.assets.filter(a => a.kind === 'sketch' && !a.binned_at).at(-1)?.id || null;
        await renderStage();
      }
      if ($('#shot-dialog').open) renderDraftStatus();
    }
    $('#save-status').textContent = 'Saved locally';
  } finally { refreshing = false; }
}

function renderBoard() {
  const p = project(); if (!p) return;
  $('#project-list').innerHTML = state.projects.map(item => `<button class="project-link ${item.id === p.id ? 'active' : ''}" data-project="${esc(item.id)}">${esc(item.title)}</button>`).join('');
  document.body.dataset.aspect=p.aspect;
  $('#project-title').textContent = p.title; $('#project-crumb').textContent = p.title;
  $('#project-subtitle').textContent = p.brief;
  const seconds = p.shots.reduce((n,s) => n + s.duration, 0), clips = p.shots.filter(s => selected(s,'clip')).length, ready = p.shots.filter(s => s.animation_ready).length;
  $('#project-stats').innerHTML = `<span class="stat"><b>${p.shots.length}</b> shots</span><span class="stat"><b>${timecode(seconds)}</b> planned</span><span class="stat"><b>${p.aspect}</b> frame</span><span class="stat"><b>${clips}/${p.shots.length}</b> clips selected</span>`;
  $('#shot-count').textContent = String(p.shots.length);
  $('#storage-label').textContent = 'Media · ' + state.storage;
  renderEpisode(); renderMusic(); renderSections();
  if(handoffPreviewBasis!==`${p.id}:${p.revision}:${$('#handoff-scene').value}`){$('#handoff-preview-label').hidden=true;$('#handoff-preview').value='';}
  $('#shot-grid').innerHTML = visibleShots().map(s => {
    const i=p.shots.findIndex(item=>item.id===s.id);
    const clipPreview = selected(s,'clip');
    const a = selected(s,'keyframe') || selected(s,'sketch') || s.assets.filter(a => a.kind !== 'clip' && !a.binned_at).at(-1) || (clipPreview?.poster_path ? {...clipPreview,path:clipPreview.poster_path,kind:'poster'} : null);
    const clip = selected(s,'clip');
    const status = clip ? 'Clip selected' : s.animation_ready ? 'Ready to animate' : selected(s,'keyframe') ? 'Review finished image' : selected(s,'sketch') ? 'Composition selected' : a ? 'Choose a composition' : 'Composition pending';
    return `<article class="shot-card"><div class="card-top"><span class="shot-id">SHOT ${String((i+1)*10).padStart(3,'0')}</span><span>${s.duration}s · ${p.aspect}</span></div><button class="card-art" data-open="${s.id}" aria-label="Open ${esc(s.title)}">${a ? `<img src="${media(a)}" alt="${esc(s.title)} — ${a.kind === 'poster' ? 'clip preview' : a.kind === 'sketch' ? 'local composition candidate' : 'finished image'}" loading="lazy"><span class="art-caption">${a.kind === 'poster' ? 'CLIP PREVIEW' : a.kind === 'sketch' ? 'COMPOSITION' : 'FINISHED IMAGE'} · TAKE ${a.take}</span>` : (blockingFrame(s,p.aspect) || empty('Make the first composition'))}</button><div class="card-body"><div class="source-label">${esc(s.scene || 'Ungrouped')} · ${storyLabel(s)}</div><h2>${esc(s.title)}</h2><p>${esc(s.purpose)}</p>${s.dialogue ? `<blockquote>${esc(s.dialogue)}</blockquote>` : ''}<span class="stage-badge ${s.animation_ready || clip ? 'ready':''}">● ${status}</span></div><div class="card-insert"><button class="quiet" data-insert="${s.id}" data-position="before" aria-label="Insert before ${esc(s.title)}">＋ Before</button><button class="quiet" data-insert="${s.id}" data-position="after" aria-label="Insert after ${esc(s.title)}">＋ After</button></div><div class="card-footer"><div class="reorder"><button class="quiet" data-move="${s.id}" data-delta="-1" aria-label="Move ${esc(s.title)} earlier" ${i===0?'disabled':''}>←</button><button class="quiet" data-move="${s.id}" data-delta="1" aria-label="Move ${esc(s.title)} later" ${i===p.shots.length-1?'disabled':''}>→</button></div><button class="quiet" data-open="${s.id}">Open shot ↗</button></div></article>`;
  }).join('');
  const jobs = state.jobs.filter(j => j.project_id === p.id).slice(-8).reverse();
  $('#jobs').hidden = !jobs.length;
  $('#jobs').innerHTML = '<h3>Local draft queue</h3>' + jobs.map(j => `<div class="job"><div><b>${esc(p.shots.find(s=>s.id===j.shot_id)?.title || j.shot_id)}</b><div class="job-status">${esc(j.status)} · ${esc(j.message)}</div></div>${['running','queued'].includes(j.status) ? `<button class="quiet" data-cancel="${j.id}">Cancel draft</button>` : ''}</div>`).join('');
  renderReferences(); renderLibrary(); syncCloudProject();
}

function renderEpisode() {
  const p=project(), scenes=[]; let elapsed=0;
  for(const s of p.shots){const name=s.scene||'Ungrouped shots'; if(!scenes.length||scenes.at(-1).name!==name)scenes.push({name,start:elapsed,seconds:0,shots:[]}); const group=scenes.at(-1);group.seconds+=s.duration;group.shots.push(s);elapsed+=s.duration;}
  const limit=Number(p.runtime_limit_seconds)||0;
  $('#episode-notes').textContent=p.episode_notes||'Group shots into scenes and record story commitments in Brief & appearance.';
  $('#runtime-budget').innerHTML=`<div class="budget-line"><b>${timecode(elapsed)} planned${limit?' / '+timecode(limit)+' ceiling':''}</b><span>${limit ? (elapsed>limit?'Over budget by '+timecode(elapsed-limit):timecode(limit-elapsed)+' available'):'No runtime ceiling set'}</span></div>${limit?`<progress max="${limit}" value="${elapsed}" aria-label="Planned episode runtime"></progress>`:''}<p class="fine">Planning allocations include reported existing beats. Actual cut length and media joins remain unverified.</p>`;
  $('#scene-map').innerHTML=scenes.map((g,i)=>`<article class="scene-card"><div class="scene-number">${String(i+1).padStart(2,'0')}</div><div><div class="eyebrow">${timecode(g.start)} — ${timecode(g.start+g.seconds)} · ${g.shots.length} SHOTS</div><h3>${esc(g.name)}</h3><p>${esc(g.shots[0].purpose)}</p><div class="fine">${g.shots.every(s=>s.story_status==='reported_existing')?'Reported beats · reconcile with existing cut':'Pilot staging proposed · story direction recorded'}</div></div><button class="secondary" data-scene="${esc(g.shots[0].scene||'')}">Open scene ↗</button></article>`).join('');
  const names=[...new Set(p.shots.map(s=>s.scene).filter(Boolean))];
  if(sceneFilter&&!names.includes(sceneFilter))sceneFilter='';
  $('#scene-filter').innerHTML='<option value="">Whole episode</option>'+names.map(n=>`<option value="${esc(n)}">${esc(n)}</option>`).join('');
  $('#scene-filter').options[0].textContent=p.music_video?'Whole music video':'Whole episode';
  $('#scene-filter').value=sceneFilter;
  const scope=$('#handoff-scene').value, options=[...names.map(n=>({value:'scene:'+n,label:n})),...p.shots.map((s,i)=>({value:'shot:'+s.id,label:`Shot ${String((i+1)*10).padStart(4,'0')} · ${s.title}`}))];
  $('#handoff-scene').innerHTML='<option value="">Whole episode</option><optgroup label="Scenes">'+options.slice(0,names.length).map(o=>`<option value="${esc(o.value)}">${esc(o.label)}</option>`).join('')+'</optgroup><optgroup label="Individual shots">'+options.slice(names.length).map(o=>`<option value="${esc(o.value)}">${esc(o.label)}</option>`).join('')+'</optgroup>';
  $('#handoff-scene').value=options.some(o=>o.value===scope)?scope:'';
  $('#board-scope').textContent=sceneFilter?`${visibleShots().length} shots · ${timecode(visibleShots().reduce((n,s)=>n+s.duration,0))} planned`:p.music_video?'Whole music video · draft timing; compare takes and add coverage':'Whole episode · diagrams are staging studies, not approved character images';
}

function renderReferences() {
  const visual=(project().visual_references||[]).map(r=>`<article class="reference-card visual-reference"><img src="${media(r)}" alt="${esc(r.title)}"><div><div class="eyebrow">USER-SUPPLIED CHARACTER & STYLE REFERENCE</div><h3>${esc(r.title)}</h3><p>${esc(r.notes)}</p><a class="button quiet" href="${media(r)}" download>Save original reference ↓</a><p class="fine">Included in Claude image packets. This is a shared appearance reference, not an approved shot starter.</p></div></article>`).join('');
  const links = project().references.map(r => `<article class="reference-card"><div class="eyebrow">RESEARCH NOTE</div><h3>${esc(r.title)}</h3><p>${esc(r.notes)}</p><a href="${esc(r.url)}" target="_blank" rel="noopener noreferrer">Open source ↗</a></article>`).join('');
  $('#reference-grid').innerHTML=visual+links || '<p class="fine">Add source links and observations through a storyboard plan import.</p>';
}
function fillDirection() { const p = project(); for (const key of ['title','brief','look','continuity','episode_notes','runtime_limit_seconds','aspect']) $('#direction-form').elements[key].value = p[key]; $('#direction-form').elements.music_video.checked=!!p.music_video; $('#aspect-note').textContent = `Project format: ${p.aspect}`; $('#direction-form').dataset.dirty = 'no'; }
function switchTab(name) { if(sectionDirty && name!=='sections'){toast('Save the song section and ranges before changing views.');return;} if(libraryDirty && name!=='library'){toast('Save or discard your review-note changes first.');return;} if(name!=='library')$('#library-player')?.pause(); if(currentTab==='direction' && $('#direction-form').dataset.dirty==='yes' && name!=='direction'){toast('Save the shared direction before changing views.');return;} currentTab = name; $$('.tabs button').forEach(b => b.classList.toggle('active', b.dataset.tab === name)); $$('.view').forEach(v=>v.hidden=v.id !== 'view-'+name); if (name === 'direction') fillDirection(); if(name==='library')renderLibrary(); if(name==='cloud')safe(()=>loadCloud(true))(); }
function fillShot() { for (const key of [...SHOT_FIELDS,'duration']) $('#shot-form').elements[key].value = shot()[key] ?? ''; dirty = false; }
async function openShot(id) { showBinnedTakes=false; shotId=id; stage=project().music_video && shot()?.assets.some(a=>a.kind==='clip')?'clip':'sketch'; previewId=null; fillShot(); await renderStage(); if(!$('#shot-dialog').open)$('#shot-dialog').showModal(); }
async function saveShot() {
  if (!$('#shot-form').reportValidity()) throw new Error('Check the shot title and duration.');
  const values = Object.fromEntries(new FormData($('#shot-form'))); values.duration = Number(values.duration);
  const p = project(), shots = p.shots.map(s=>s.id===shotId ? {...s,...values} : s);
  const result = await api('save', {id:p.id, revision:p.revision, shots}); accept(result.project); dirty=false;
  await renderStage(); toast('Shot brief saved');
}
async function insertShot(anchor, position) {
  if (currentTab==='direction' && $('#direction-form').dataset.dirty==='yes') throw new Error('Save the shared direction before inserting.');
  if (dirty) await saveShot();
  const p=project(), result=await api('insert',{project:p.id,anchor,position,revision:p.revision});
  accept(result.project); sceneFilter=shotScene(result.shot_id); renderBoard(); switchTab('board');
  await openShot(result.shot_id); toast('New draft inserted '+position+' the target shot.');
}
function shotScene(id) { return sceneFilter ? project().shots.find(s=>s.id===id)?.scene || '' : ''; }
function renderDraftStatus() {
  const status = $('#draft-status'), button = $('#render-draft');
  if (!status) return;
  const job = (state.jobs || []).filter(j => j.project_id === projectId && j.shot_id === shotId).sort((a,b) => b.created_at.localeCompare(a.created_at))[0];
  const active = job && ['queued','running'].includes(job.status);
  if (button) {
    button.disabled = submittingDraft || !!active || !['0.5','0.6','0.7'].includes(state.version);
    button.title=!['0.5','0.6','0.7'].includes(state.version)?'Drawing-route update is being installed. Reload after the server restarts.':'';
    button.textContent = submittingDraft ? 'Submitting…' : active ? job.status === 'running' ? 'Drawing locally…' : 'Draft queued…' : 'Draw locally';
  }
  status.hidden = stage !== 'sketch' || (!job && !submittingDraft);
  if (status.hidden) return;
  if (submittingDraft) { status.textContent = 'Sending this draft to the local queue…'; return; }
  const start = Date.parse(job.started_at || job.created_at);
  const seconds = job.elapsed_seconds ?? Math.max(0, Math.floor(((job.ended_at ? Date.parse(job.ended_at) : Date.now()) - start) / 1000));
  const pressure = job.samples?.at(-1)?.pressure;
  const binnedResult=shot()?.assets.some(a=>a.local_generation?.job_id===job.id&&a.binned_at);
  const labels = {queued:'Queued',running:'Drawing locally',done:'Draft saved',blocked:'Draft stopped',cancelled:'Draft cancelled',interrupted:'Draft interrupted'};
  status.dataset.state = job.status;
  status.innerHTML = `<div><b>${esc(binnedResult?'Draft binned':labels[job.status] || job.status)} · ${timecode(seconds)}</b><span>${esc(binnedResult?'Rejected take retained in the bin.':job.message)}${active && pressure ? ' Memory · ' + esc(pressure) + '.' : ''}</span></div>${active ? `<button class="quiet" data-cancel="${esc(job.id)}">Cancel draft</button>` : ''}`;
}
async function renderStage() {
  const p = project(), s = shot(); if (!s) return;
  const index = p.shots.findIndex(item=>item.id === s.id);
  $('#shot-number').textContent = `SHOT ${String((index+1)*10).padStart(3,'0')} · ${s.duration} SECONDS`;
  $('#shot-heading').textContent = s.title;
  $('#shot-prev').disabled=index===0; $('#shot-next').disabled=index===p.shots.length-1;
  $$('.mini-tabs button').forEach(b=>b.classList.toggle('active',b.dataset.stage===stage));
  const stageAssets=s.assets.filter(a=>a.kind===stage);
  const assets=stageAssets.filter(a=>showBinnedTakes || !a.binned_at);
  let a = assets.find(a=>a.id===previewId) || selected(s,stage) || assets.at(-1);
  previewId = a?.id || null;
  const help = {sketch:'Explore framing and subject placement. Review local drafts for how clearly they communicate the shot.',keyframe:'Use your selected composition and shared appearance references in an image app. Import the finished image and approve it before animation.',clip:'Attach the approved finished image to your video app with the motion prompt. Import its clips here, compare takes and select one for the sequence.'};
  $('#stage-help').textContent = help[stage];
  $('#asset-preview').innerHTML = a ? stage==='clip' ? `<video controls preload="metadata" playsinline src="${media(a)}"></video>` : `<img src="${media(a)}" alt="${esc(s.title)} ${stage} take ${a.take}">` : stage==='sketch' && s.blocking ? blockingFrame(s,p.aspect) : empty(stage==='sketch'?'No composition yet':stage==='keyframe'?'Import a finished starter image':'Import a generated clip');
  showClipRange(a?.binned_at?null:a);
  $('#asset-takes').innerHTML = assets.map(item=>`<button class="take ${item.id === previewId ? 'selected':''}" data-preview="${item.id}" aria-label="Preview ${stage} take ${item.take}">${stage==='clip'?(item.poster_path?`<img src="${media({path:item.poster_path})}" alt="Clip take ${item.take}">`:'<span class="clip-icon">▷</span>'):`<img src="${media(item)}" alt="Take ${item.take}">`}<span>Take ${item.take} · ${esc(item.provider)}${item.source_ref?' · Library range':''}${item.binned_at?' · Binned':''}</span>${s['selected_'+stage]===item.id?'<b>Selected</b>':''}</button>`).join('');
  if(stageAssets.some(item=>item.binned_at))$('#asset-takes').insertAdjacentHTML('beforeend',`<button class="quiet" id="toggle-take-bin">${showBinnedTakes?'Hide':'Show'} bin (${stageAssets.filter(item=>item.binned_at).length})</button>`);
  const chosen = a && s['selected_'+stage] === a.id;
  let actions = `<button class="secondary" data-upload="${stage}">＋ Import ${stage==='sketch'?'composition':stage==='keyframe'?'finished image':'clip'}</button>`;
  if (stage==='sketch') actions += '<button class="primary" id="render-draft">Draw locally</button>';
  if (a && !a.binned_at) actions += `<button class="${chosen?'quiet':'primary'}" id="select-asset">${chosen ? '✓ Selected' : stage==='clip'?'Use this take':stage==='sketch'?'Select composition':'Select starter image'}</button><a class="button quiet" href="${media(a)}" download="${String((index+1)*10).padStart(4,'0')}_${s.id}_${stage}_take${String(a.take).padStart(2,'0')}${a.path.slice(a.path.lastIndexOf('.'))}">Save file ↓</a>`;
  if (stage === 'keyframe' && a && !a.binned_at) actions += `<button class="${s.animation_ready && chosen?'quiet':'primary'}" id="approve-keyframe">${s.animation_ready&&chosen?'✓ Approved for animation':'Approve for animation'}</button>`;
  if (stage === 'clip') {
    actions += '<button class="secondary" id="browse-library">Browse project footage</button>';
    const urls={grok:'https://grok.com/',flow:'https://labs.google/fx/tools/flow',gemini:'https://gemini.google.com/'};
    if(urls[$('#provider').value]) actions += `<a class="button quiet" href="${urls[$('#provider').value]}" target="_blank" rel="noopener">Open ${esc($('#provider').selectedOptions[0].text)} ↗</a>`;
  }
  if(a)actions+=`<button class="quiet" id="${a.binned_at?'restore-take':'bin-take'}">${a.binned_at?'Restore take':'Bin take'}</button>`;
  if(a?.binned_at)actions+='<span class="fine">Binned · original file and provenance retained</span>';
  if(stage==='keyframe'||stage==='clip')actions+='<button class="secondary" id="open-cloud-shot">'+(stage==='clip'?'Generate with Veo':'Create with Google')+'</button>';
  $('#asset-actions').innerHTML = actions;
  renderLocalDrawing();
  renderDraftStatus();
  const packageData = await api('prompts',{project:p.id,shot:s.id,provider:$('#provider').value});
  promptText = packageData[{sketch:'local_draft',keyframe:'enhancement',clip:'animation'}[stage]];
  $('#prompt-title').textContent = {sketch:'Local drawing prompt',keyframe:'Image treatment prompt',clip:'Animation prompt'}[stage];
  $('#prompt-text').value = promptText;
  $('#copy-prompt').disabled = stage==='clip' && (!s.animation_ready || ($('#provider').value==='grok' && packageData.direction_review && !packageData.optimized_direction_ready));
  let note = stage==='sketch'?`One local draft at a time · ${p.aspect==='9:16'?'576 × 1024 portrait':'1024 × 576 landscape'} · 4 steps · rough layouts / image-to-image variations; character identity is not locked.`:stage==='keyframe'?'Attach the selected sketch and appearance references manually. Review whether the composition survives.':s.animation_ready?'Ready: attach the approved starter image. Match the app’s supported duration and review the generated motion.':'Select and approve a finished image in step 2 to prepare this animation handoff.';
  if (stage==='clip' && a && a.keyframe_id !== s.selected_keyframe) note += ' This clip was imported against a different or unrecorded starter image; review its continuity.';
  if (stage==='clip' && packageData.direction_review) {
    const r=packageData.direction_review;
    note += packageData.optimized_direction_ready ? ' Using the imported Grok direction for this approved starter.' : packageData.direction_review_status==='stale' ? ' The imported direction needs a fresh Claude review after the image or brief changes.' : r.verdict==='needs_image_fix' ? ' Imported review: correct the starter image before animation.' : ' Imported direction is provisional until checked against an approved finished starter.';
    if(r.image_fixes)note+=' Image notes: '+r.image_fixes;
    if(r.reference_application)note+=' Reference application: '+r.reference_application;
  }
  $('#prompt-note').textContent = note;
}
async function uploadFile(file) {
  if (dirty) await saveShot();
  if (file.size > 256*1024*1024) throw new Error('Use a file smaller than 256 MiB.');
  const q = new URLSearchParams({project:projectId,shot:shotId,kind:stage,filename:file.name,provider:stage==='sketch'?'manual':$('#provider').value});
  toast('Saving the imported file…');
  const r = await fetch('/api/upload?'+q,{method:'POST',headers:{'X-Storyboard-Token':state.token},body:file});
  const data=await r.json(); if(!r.ok)throw new Error(data.error);
  accept(data.project); previewId=shot().assets.filter(a=>a.kind===stage).at(-1)?.id; await renderStage(); toast('Take saved. Review it before selecting.');
}

function stopReview() { stopMusicReview(); const video=$('#review-screen video'); if(video){video.pause();video.removeAttribute('src');video.load();} }
function renderReview(auto = false) {
  if (musicReviewActive) { seekMusicReview(reviewIndex); return; }
  stopReview(); const p=project(), s=reviewList[reviewIndex], a=selected(s,'clip');
  $('#review-mode-label').textContent=reviewMode==='board'?'STORYBOARD READ-THROUGH':'THE SELECTED SEQUENCE';
  $('#review-help').textContent=reviewMode==='board'?'Use Previous / Next to read the board. Diagrams and text are planning aids; this is not rendered video or recorded audio.':'Playback follows storyboard order, stops at missing clips and uses the target edit duration. Exported source clips remain untrimmed.';
  $('#review-heading').textContent = `${p.title} · ${s.title}`;
  $('#review-position').textContent = `${reviewIndex+1} / ${reviewList.length} · ${s.duration}s target`;
  $('#review-prev').disabled=reviewIndex===0; $('#review-next').disabled=reviewIndex===reviewList.length-1;
  $('#review-strip').innerHTML=reviewList.map((item,i)=>`<button data-review="${i}" class="${i===reviewIndex?'active':''}">${String((p.shots.findIndex(s=>s.id===item.id)+1)*10).padStart(3,'0')} · ${esc(item.title)} ${selected(item,'clip')?'●':'○'}</button>`).join('');
  $('#review-caption').innerHTML=`<div class="eyebrow">${esc(s.scene||'')} · ${storyLabel(s)}</div>${[['Dialogue',s.dialogue],['Voiceover',s.narration],['Action',s.action],['Audience',s.audience],['Intended anomaly',s.anomaly]].filter(([k,value])=>value).map(([k,value])=>`<p><b>${k}</b> ${esc(value)}</p>`).join('')}`;
  if(reviewMode==='board'){const still=selected(s,'keyframe')||selected(s,'sketch');$('#review-screen').innerHTML=still?`<img src="${media(still)}" alt="Selected still for ${esc(s.title)}">`:blockingFrame(s,p.aspect)||empty('No visual plan');return;}
  if(a){
    $('#review-screen').innerHTML=`<video controls playsinline preload="metadata" src="${media(a)}"></video>`;
    const v=$('#review-screen video'); let advanced=false;
    const clipStart=a.in_seconds||0;
    v.addEventListener('loadedmetadata',()=>{v.currentTime=clipStart;});
    if(p.music_video){v.muted=true;v.volume=0;}
    const next=()=>{if(advanced)return;advanced=true;if(reviewIndex<reviewList.length-1){reviewIndex++;renderReview(true);}else{v.pause();toast('End of the selected sequence');}};
    v.addEventListener('ended',next);v.addEventListener('timeupdate',()=>{if(v.currentTime-clipStart>=s.duration || (a.out_seconds!=null && v.currentTime>=a.out_seconds))next();});
    if(auto)v.play().catch(()=>toast('Press play to start this clip.'));
  }else{
    const still=selected(s,'keyframe')||selected(s,'sketch');
    $('#review-screen').innerHTML=still?`<img src="${media(still)}" alt="Selected still for ${esc(s.title)}"><div class="review-label">STILL PREVIEW · CLIP PENDING</div>`:`<div class="review-empty"><span class="eyebrow">SHOT ${String((reviewIndex+1)*10).padStart(3,'0')}</span><h3>${esc(s.title)}</h3><p>${esc(s.purpose)}</p><p>No selected clip yet. Playback pauses here so the missing shot stays visible.</p></div>`;
  }
}

document.addEventListener('click',safe(async e=>{
  const b=e.target.closest('button');if(!b)return;
  if(b.classList.contains('close-dialog')){const d=b.closest('dialog');if(d.id==='shot-dialog'&&dirty)await saveShot();d.close();return;}
  if(b.dataset.scene!==undefined){sceneFilter=b.dataset.scene;renderBoard();switchTab('board');return;}
  if(b.dataset.project){if(sectionDirty){toast('Save the song section and ranges before changing projects.');return;}if(libraryDirty){toast('Save or discard your review-note changes first.');return;}if(currentTab==='direction'&&$('#direction-form').dataset.dirty==='yes'){toast('Save the shared direction before changing projects.');return;}if(dirty)await saveShot();projectId=b.dataset.project;sceneFilter='';localStorage.setItem('storyboard-project',projectId);history.replaceState(null,'','?project='+encodeURIComponent(projectId));renderBoard();fillDirection();return;}
  if(b.dataset.tab){switchTab(b.dataset.tab);return;}
  if(b.dataset.open){await openShot(b.dataset.open);return;}
  if(b.dataset.insert){await insertShot(b.dataset.insert,b.dataset.position);return;}
  if(b.dataset.move){const p=project(), shots=[...p.shots],i=shots.findIndex(s=>s.id===b.dataset.move),j=i+Number(b.dataset.delta);[shots[i],shots[j]]=[shots[j],shots[i]];accept((await api('save',{id:p.id,revision:p.revision,shots})).project);toast('Storyboard order saved');return;}
  if(b.dataset.stage){if(dirty)await saveShot();stage=b.dataset.stage;previewId=null;await renderStage();return;}
  if(b.dataset.preview){previewId=b.dataset.preview;await renderStage();return;}
  if(b.dataset.upload){$('#asset-file').accept=stage==='clip'?'.mp4,.mov,.webm':'.png,.jpg,.jpeg,.webp';$('#asset-file').value='';$('#asset-file').click();return;}
  if(b.dataset.cancel){await api('cancel',{job:b.dataset.cancel});await refresh();toast('Draft cancellation requested');return;}
  if(b.dataset.review!==undefined){reviewIndex=Number(b.dataset.review);renderReview(true);return;}
  switch(b.id){
    case 'render-draft':
      if(!['0.5','0.6','0.7'].includes(state.version))throw Error('The drawing-route update needs the restarted server. Reload after installation.');
      if(submittingDraft || (state.jobs || []).some(j => j.project_id===projectId && j.shot_id===shotId && ['queued','running'].includes(j.status))) return;
      submittingDraft=true; renderDraftStatus();
      try { if(dirty)await saveShot();await api('render',{project:projectId,shot:shotId,...localDrawingRequest()});toast('Local draft queued. Its status appears above the image.');await refresh(); }
      finally { submittingDraft=false;renderDraftStatus(); }
      break;
    case 'toggle-take-bin': showBinnedTakes=!showBinnedTakes;previewId=null;await renderStage();break;
    case 'bin-take':
    case 'restore-take':
      if(dirty)await saveShot();
      accept((await api('bin',{project:projectId,shot:shotId,asset:previewId,revision:project().revision,restore:b.id==='restore-take',reason:b.id==='bin-take'?'Rejected during take review.':''})).project);
      previewId=null;await renderStage();toast(b.id==='restore-take'?'Take restored; select it when ready.':'Take binned. Original file retained.');break;
    case 'select-asset': if(dirty)await saveShot();accept((await api('select',{project:projectId,shot:shotId,kind:stage,asset:previewId})).project);await renderStage();toast(stage==='clip'?'Take selected for the sequence':'Selection saved');break;
    case 'approve-keyframe': if(dirty)await saveShot();accept((await api('select',{project:projectId,shot:shotId,kind:'keyframe',asset:previewId,approve:true})).project);await renderStage();toast('Starter image approved for animation');break;
    case 'copy-prompt': if(dirty)await saveShot();await copyText(promptText);break;
    case 'read-board': reviewMode='board';reviewList=[...project().shots];reviewIndex=0;renderReview();$('#review-dialog').showModal();break;
    case 'read-scene': reviewMode='board';reviewList=[...visibleShots()];reviewIndex=0;renderReview();$('#review-dialog').showModal();break;
    case 'download-script': download('episode-storyboard.txt',(await api('script',{project:projectId})).text);break;
    case 'shot-insert-before': await insertShot(shotId,'before');break;
    case 'shot-insert-after': await insertShot(shotId,'after');break;
    case 'shot-prev': case 'shot-next': {if(dirty)await saveShot();const i=project().shots.findIndex(s=>s.id===shotId);await openShot(project().shots[i+(b.id==='shot-prev'?-1:1)].id);break;}
    case 'play-sequence': if(startMusicReview())break; reviewMode='clips';reviewList=[...project().shots];reviewIndex=0;renderReview(true);$('#review-dialog').showModal();break;
    case 'review-prev': reviewIndex=Math.max(0,reviewIndex-1);renderReview(true);break;
    case 'review-next': reviewIndex=Math.min(reviewList.length-1,reviewIndex+1);renderReview(true);break;
    case 'export-project': {toast('Preparing the selected assets and prompts…');const r=await api('export',{project:projectId});const a=document.createElement('a');a.href=r.url;a.download='storyboard-review.zip';a.click();toast('Review package prepared');break;}
    case 'copy-intake': await copyText((await api('intake',{project:projectId,...handoffScope()})).text);break;
    case 'preview-claude': case 'copy-claude': {const text=(await api('claude',{project:projectId,text_only:true,...handoffScope()})).text;$('#handoff-preview').value=text;handoffPreviewBasis=`${projectId}:${project().revision}:${$('#handoff-scene').value}`;$('#handoff-preview-label').hidden=false;if(b.id==='copy-claude')await copyText(text);break;}
    case 'export-claude': {const r=await api('claude',{project:projectId,...handoffScope()});const a=document.createElement('a');a.href=r.url;a.download='claude-direction.zip';a.click();toast('Claude packet prepared. Unzip and attach its images with the brief.');break;}
    case 'download-plan': download('storyboard-plan.json',JSON.stringify(plan(project()),null,2),'application/json');break;
    case 'apply-plan': {const raw=JSON.parse($('#plan-json').value);const p=project();accept((await api('plan',{project:p.id,revision:p.revision,plan:raw})).project);fillDirection();toast('Plan applied. Other shots and saved media were retained.');break;}
    case 'import-plan-file': $('#plan-file').value='';$('#plan-file').click();break;
    case 'new-project': $('#new-dialog').showModal();break;
    case 'import-template': $('#template-file').value='';$('#template-file').click();break;
    case 'add-shot': await insertShot(visibleShots().at(-1)?.id || project().shots.at(-1).id,'after');break;
  }
}));
$('#handoff-scene').addEventListener('change',()=>{$('#handoff-preview-label').hidden=true;$('#handoff-preview').value='';});
$('#scene-filter').addEventListener('change',e=>{sceneFilter=e.target.value;renderBoard();});
$('#shot-form').addEventListener('input',()=>dirty=true);
$('#shot-form').addEventListener('submit',safe(async e=>{e.preventDefault();await saveShot();}));
$('#provider').addEventListener('change',safe(async()=>{if(dirty)await saveShot();else await renderStage();}));
$('#direction-form').addEventListener('input',()=>$('#direction-form').dataset.dirty='yes');
$('#direction-form').addEventListener('submit',safe(async e=>{e.preventDefault();const p=project();accept((await api('save',{id:p.id,revision:p.revision,...Object.fromEntries(new FormData(e.target)),music_video:e.target.elements.music_video.checked})).project);$('#direction-form').dataset.dirty='no';toast('Shared direction saved');}));
$('#new-form').addEventListener('submit',safe(async e=>{e.preventDefault();const raw=Object.fromEntries(new FormData(e.target));raw.music_video=e.target.elements.music_video.checked;raw.shots=[{title:'The opening',duration:5,purpose:'Establish the story.'}];await createProject(raw);}));
$('#template-file').addEventListener('change',safe(async e=>{
  const file=e.target.files[0];if(!file)return;if(file.size>1024*1024)throw new Error('Use a template smaller than 1 MiB.');
  const raw=JSON.parse(await file.text());
  if(raw.template_kind!=='storyboard-project-v1')throw new Error('Choose a storyboard project template. Revised plans belong in Buckley & handoffs.');
  await createProject(raw);
}));
$('#asset-file').addEventListener('change',safe(async e=>{if(e.target.files[0])await uploadFile(e.target.files[0]);}));
$('#plan-file').addEventListener('change',safe(async e=>{if(e.target.files[0]){if(e.target.files[0].size>1024*1024)throw new Error('Plan JSON must be smaller than 1 MiB.');$('#plan-json').value=await e.target.files[0].text();toast('Plan loaded. Review it, then apply.');}}));
$('#review-dialog').addEventListener('close',stopReview);
$('#shot-dialog').addEventListener('close',()=>{const v=$('#asset-preview video');if(v)v.pause();});
$('#shot-dialog').addEventListener('cancel',e=>{if(dirty){e.preventDefault();toast('Save the shot brief before closing.');}});
window.addEventListener('beforeunload',e=>{if(dirty||libraryDirty||$('#direction-form').dataset.dirty==='yes'){e.preventDefault();e.returnValue='';}});
async function updateMetrics(){try{const r=await fetch('/api/metrics');const m=await r.json();const pressure=m.pressure||'unavailable';$('#memory').className='meter '+pressure;$('#memory').textContent='Memory · '+pressure;}catch(_){$('#memory').textContent='Memory · unavailable';}}
safe(async()=>{await refresh(true);updateMetrics();setInterval(()=>refresh().catch(()=>$('#save-status').textContent='Disconnected · use the launcher to reopen'),8000);setInterval(updateMetrics,20000);})();
