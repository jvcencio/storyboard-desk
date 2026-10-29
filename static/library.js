'use strict';

let librarySourceId = '', libraryProjectId = '', libraryPlayerKey = '', libraryTargetId = '';
let libraryDirty = false, libraryRangePlaying = false;
let libraryGridKey = '';

function librarySources(p = project()) {
  const sources = [];
  for (const s of p.shots) for (const a of s.assets) {
    if (a.kind !== 'clip' || a.source_ref || a.binned_at) continue;
    const uses = p.shots.flatMap(target => {
      const chosen = selected(target, 'clip');
      return chosen && (chosen.sha256 ? chosen.sha256 === a.sha256 : chosen.path === a.path)
        ? [{shot:target, asset:chosen}] : [];
    });
    const sectionUses=(p.music_sections||[]).filter(section=>sectionHasSource(section,a));
    sources.push({shot:s, asset:a, uses, sectionUses});
  }
  return sources;
}

function librarySource() { return librarySources().find(r => r.asset.id === librarySourceId); }
function sourceName(a) { return (a.original_name || 'Imported clip').replace(/_[a-f0-9]{8,}\.[^.]+$/i, '').replace(/_/g, ' '); }

function renderLibrary() {
  if (!$('#library-grid') || !project()) return;
  const p = project(), sources = librarySources(p);
  if (libraryProjectId !== p.id) {
    libraryProjectId = p.id; librarySourceId = ''; libraryPlayerKey = ''; libraryTargetId = ''; libraryDirty = false;
  }
  const query = $('#library-search').value.trim().toLowerCase();
  const chosenSetup = $('#library-setup-filter').value;
  const setups = [...new Set(sources.map(r => r.asset.editorial_review?.setup || r.asset.source_record?.coverage_group || '').filter(Boolean))].sort();
  const options = '<option value="">All setups</option>' + setups.map(v => `<option value="${esc(v)}">${esc(v)}</option>`).join('');
  if ($('#library-setup-filter').innerHTML !== options) { $('#library-setup-filter').innerHTML = options; $('#library-setup-filter').value = setups.includes(chosenSetup) ? chosenSetup : ''; }
  const pickedSection=$('#library-section-filter').value;
  $('#library-section-filter').innerHTML='<option value="">All sections</option>'+(p.music_sections||[]).map(s=>`<option value="${s.id}">${esc(s.label)}</option>`).join('');
  $('#library-section-filter').value=(p.music_sections||[]).some(s=>s.id===pickedSection)?pickedSection:'';
  const sectionFilter=$('#library-section-filter').value;
  const setup = $('#library-setup-filter').value, status = $('#library-status-filter').value;
  const visible = sources.filter(({shot:s, asset:a, uses, sectionUses}) => {
    const review = a.editorial_review || {};
    const words = [sourceName(a), s.title, s.scene, review.setup, review.phase, review.note,
      a.source_record?.description, a.source_record?.notes, a.source_record?.coverage_group, ...sectionUses.flatMap(section=>[section.label,section.kind,...sectionCoverage(section).filter(c=>sectionCoverageKey(c)===sectionAssetKey(a)).map(c=>c.music_tag||'none')])].join(' ').toLowerCase();
    return (!sectionFilter || sectionUses.some(section=>section.id===sectionFilter)) && (!query || words.includes(query)) && (!setup || setup === (review.setup || a.source_record?.coverage_group))
      && (!status || (status === 'unused' ? !uses.length : (review.status || 'pending') === status));
  });
  if (!visible.some(r => r.asset.id === librarySourceId) && !libraryDirty) librarySourceId = visible[0]?.asset.id || '';
  $('#library-summary').textContent = `${sources.length} source clips · ${visible.length} shown · one media file can serve several shots`;
  const last = [...(p.placement_history || [])].reverse().find(e => !e.undone_at);
  $('#undo-library-placement').disabled = !last || libraryDirty;
  const gridKey = [p.id,p.revision,query,setup,status,sectionFilter,librarySourceId].join('|');
  if (libraryGridKey !== gridKey) $('#library-grid').innerHTML = visible.map(({shot:s, asset:a, uses}) => `<button class="library-card ${a.id === librarySourceId ? 'active' : ''}" data-library-source="${a.id}" aria-label="Review ${esc(sourceName(a))}">
    ${a.poster_path ? `<img src="${media({path:a.poster_path})}" alt="" loading="lazy">` : '<span class="clip-icon">▷</span>'}
    <span class="library-card-title">${esc(sourceName(a))}</span><span>${esc(a.editorial_review?.setup || a.source_record?.coverage_group || s.title)}</span><span class="section-tags">${(p.music_sections||[]).filter(section=>sectionHasSource(section,a)).map(section=>esc(section.label)+' / '+esc(sectionKindName(sectionCoverage(section).find(c=>sectionCoverageKey(c)===sectionAssetKey(a))?.music_tag))).join(' · ')}${sectionReservation(a)?' · Reserved for '+esc(sectionReservation(a).label):''}</span>
    <small>${a.duration_seconds ? musicClock(a.duration_seconds) : 'Duration pending'} · ${uses.length ? `${uses.length} selected use${uses.length === 1 ? '' : 's'}` : 'Unused'} · ${esc({pending:'Needs review',candidate:'Candidate',avoid:'Avoid'}[a.editorial_review?.status || 'pending'])}</small></button>`).join('') || '<p class="fine">No clips match these filters.</p>';
  libraryGridKey = gridKey;
  const row = librarySource();
  if (!row) { $('#library-detail video')?.pause(); libraryPlayerKey = ''; $('#library-detail').innerHTML = `<p class="fine">${sources.length ? 'No source matches these filters.' : 'Import clips through a shot to begin building this library.'}</p>`; return; }
  const a = row.asset, key = `${p.id}:${a.id}`;
  if (libraryPlayerKey !== key) {
    $('#library-detail video')?.pause();
    libraryPlayerKey = key; libraryRangePlaying = false;
    const review = a.editorial_review || {};
    $('#library-detail').innerHTML = `<div class="eyebrow">SOURCE FOOTAGE</div><h2>${esc(sourceName(a))}</h2>
      <video id="library-player" controls playsinline preload="metadata" ${p.music_video ? 'muted' : ''} src="${media(a)}" ${a.poster_path ? `poster="${media({path:a.poster_path})}"` : ''}></video>
      <p class="fine">${esc(a.source_record?.description || '')} ${esc(a.source_record?.notes || '')}</p>
      <p class="fine">${a.source_record ? 'Imported assessment uses sampled frames; check playback and the tail before using a range.' : 'Review the movement, identity and ending before choosing a range.'}</p>
      <div class="library-range"><label>In (seconds)<input id="library-in" type="number" min="0" step="0.01" value="${a.in_seconds || 0}"></label><label>Out (seconds)<input id="library-out" type="number" min="0.01" step="0.01" value="${a.out_seconds ?? a.duration_seconds ?? ''}"></label></div>
      <div class="button-row"><button id="library-mark-in" class="quiet">In at playhead</button><button id="library-mark-out" class="quiet">Out at playhead</button><button id="library-play-range" class="secondary">Play range</button></div>
      <label>Place in shot<select id="library-target"><option value="">Choose a shot…</option>${p.shots.map((s,i) => `<option value="${s.id}">${String((i+1)*10).padStart(3,'0')} · ${esc(s.title)} · ${esc(s.scene || '')}</option>`).join('')}</select></label>
      <p id="library-range-summary" class="fine"></p><button id="library-place" class="primary">Use this range in shot</button>
      <p class="fine">Each use keeps its own In/Out range. The source file and other shots stay unchanged.</p><div id="library-uses" class="library-uses"></div>
      <details class="library-notes"><summary>Review notes &amp; tags</summary><form id="library-review-form"><label>Review status<select id="library-review-status"><option value="pending">Needs review</option><option value="candidate">Candidate</option><option value="avoid">Avoid</option></select></label><label>Setup<input id="library-review-setup" maxlength="100" value="${esc(review.setup || a.source_record?.coverage_group || '')}" placeholder="Wide, reaction, crew insert…"></label><label>Progression / performance<input id="library-review-phase" maxlength="100" value="${esc(review.phase || '')}" placeholder="Recognition, warming up, dancing, unconfirmed…"></label><label>Review note<textarea id="library-review-note" rows="3" maxlength="2000">${esc(review.note || '')}</textarea></label><div class="button-row"><button class="secondary" type="submit">Save review notes</button><button id="library-discard-notes" class="quiet" type="button">Discard note changes</button></div></form></details>`;
    $('#library-review-status').value = review.status || 'pending';
    $('#library-detail').scrollTop = 0;
    $('#library-target').value = libraryTargetId;
    const video = $('#library-player');
    const silence = () => { if (p.music_video) { video.muted = true; video.volume = 0; } };
    silence(); video.addEventListener('volumechange', silence);
    video.addEventListener('loadedmetadata', () => {
      if (!$('#library-out').value) $('#library-out').value = video.duration;
      $('#library-in').max = video.duration; $('#library-out').max = video.duration;
      updateLibraryRange();
    });
    video.addEventListener('timeupdate', () => { if (libraryRangePlaying && video.currentTime >= Number($('#library-out').value)) { video.pause(); libraryRangePlaying = false; } });
  }
  $('#library-uses').innerHTML = '<b>Selected in the cut</b>' + (row.uses.length ? row.uses.map(({shot:s,asset:use}) => `<div><button class="quiet" data-library-open="${s.id}">${esc(s.title)} ↗</button><span>${musicClock(use.in_seconds || 0)}–${musicClock(use.out_seconds ?? use.duration_seconds)}</span></div>`).join('') : '<p class="fine">Not currently selected in any shot.</p>');
  updateLibraryRange(); renderLibrarySectionTools();
}

function updateLibraryRange() {
  if (!$('#library-place')) return;
  const start = Number($('#library-in').value), end = Number($('#library-out').value);
  const target = project().shots.find(s => s.id === $('#library-target').value);
  const valid = Number.isFinite(start) && Number.isFinite(end) && start >= 0 && end > start;
  const owner=librarySource()&&sectionReservation(librarySource().asset),blocked=!!owner&&target&&(target.scene||'').toLowerCase()!==(owner.story_scene||owner.label).toLowerCase();
  $('#library-place').disabled = !valid || !target || libraryDirty || blocked;
  $('#library-range-summary').textContent = valid
    ? `${musicClock(end-start)} marked${target ? ` · ${target.duration}s shot allocation${end-start < target.duration ? ` · ${(target.duration-(end-start)).toFixed(1)}s stays a picture gap` : ' · source range can be cut to the allocation'}` : ' · choose its destination'}${blocked?' · Reserved for '+owner.label+'; release the reservation to place elsewhere.':''}`
    : 'Choose an In/Out range within this source.';
}

function openMediaLibrary(targetId = '', sourceId = '') {
  if (libraryDirty) { toast('Save or discard your review-note changes first.'); return; }
  libraryTargetId = targetId;
  if (sourceId) {
    const a = project().shots.flatMap(s => s.assets).find(a => a.id === sourceId);
    librarySourceId = a?.source_ref?.asset_id || sourceId;
    // A shot's source must stay visible even if an earlier search excluded it.
    $('#library-search').value = '';
    $('#library-setup-filter').value = '';
    $('#library-status-filter').value = ''; $('#library-section-filter').value = '';
  }
  libraryPlayerKey = '';
  switchTab('library'); renderLibrary();
}

document.addEventListener('DOMContentLoaded', () => {
  for (const selector of ['#library-search','#library-setup-filter','#library-status-filter','#library-section-filter']) $(selector).addEventListener('input', renderLibrary);
  document.addEventListener('input', e => {
    if (e.target.closest('#library-review-form')) { libraryDirty = true; $('#undo-library-placement').disabled = true; }
    if (e.target.id === 'library-target') libraryTargetId = e.target.value;
    if (e.target.closest('#library-detail')) updateLibraryRange();
  });
  document.addEventListener('submit', safe(async e => {
    if (e.target.id !== 'library-review-form') return;
    e.preventDefault(); const row = librarySource(), p = project();
    const result = await api('review-clip', {project:p.id, shot:row.shot.id, asset:row.asset.id, revision:p.revision,
      status:$('#library-review-status').value, setup:$('#library-review-setup').value,
      phase:$('#library-review-phase').value, note:$('#library-review-note').value});
    libraryDirty = false; accept(result.project); toast('Review notes saved on this source.');
  }));
  document.addEventListener('click', safe(async e => {
    const b = e.target.closest('button'); if (!b) return;
    if (b.dataset.librarySource) {
      if (libraryDirty) { toast('Save or discard your review-note changes first.'); return; }
      librarySourceId = b.dataset.librarySource; renderLibrary(); return;
    }
    if (b.dataset.libraryOpen) {
      if (libraryDirty) { toast('Save or discard your review-note changes first.'); return; }
      $('#library-player')?.pause(); await openShot(b.dataset.libraryOpen); return;
    }
    if (b.id === 'browse-library') { if (dirty) await saveShot(); const id = shotId, aid = previewId; $('#shot-dialog').close(); openMediaLibrary(id, aid); }
    if (b.id === 'library-discard-notes') { libraryDirty = false; libraryPlayerKey = ''; renderLibrary(); }
    if (b.id === 'library-mark-in' || b.id === 'library-mark-out') { $(`#library-${b.id === 'library-mark-in' ? 'in' : 'out'}`).value = $('#library-player').currentTime.toFixed(2); updateLibraryRange(); }
    if (b.id === 'library-play-range') { const v = $('#library-player'); v.currentTime = Number($('#library-in').value); libraryRangePlaying = true; await v.play(); }
    if (b.id === 'library-place') {
      const row = librarySource(), p = project(); b.disabled = true;
      try {
        const target = $('#library-target').value;
        const result = await api('place-clip', {project:p.id, source_shot:row.shot.id, source_asset:row.asset.id,
          target_shot:target, revision:p.revision, in_seconds:Number($('#library-in').value), out_seconds:Number($('#library-out').value), duration_seconds:$('#library-player').duration});
        accept(result.project); toast(`Range placed in ${project().shots.find(s => s.id === target).title}. Other uses are unchanged.`);
      } finally { updateLibraryRange(); }
    }
    if (b.id === 'undo-library-placement') { const p = project(); accept((await api('undo-placement', {project:p.id, revision:p.revision})).project); toast('Previous shot selection restored. Source and alternatives retained.'); }
  }));
});
