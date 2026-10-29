'use strict';

let musicReviewActive = false, musicFrame = 0, musicScreenKey = '';
const musicClock = value => {
  const n = Math.max(0, Number(value) || 0);
  return `${Math.floor(n / 60)}:${(n % 60).toFixed(1).padStart(4, '0')}`;
};

function renderMusic() {
  const p = project(), active = !!p.music_video;
  document.body.dataset.music = String(active);
  $('#music-bar').hidden = !active;
  $('#music-tab').hidden = !active;
  $('[data-tab="episode"]').textContent = active ? 'Sequence map' : 'Episode map';
  const player = $('#music-player');
  const url = p.soundtrack ? media(p.soundtrack) : '';
  if ((player.getAttribute('src') || '') !== url) {
    player.pause();
    if (url) player.setAttribute('src', url); else player.removeAttribute('src');
    player.load();
  }
  player.hidden = !p.soundtrack;
  if (!active) {
    if (currentTab === 'music') switchTab('board');
    $('#music-lyrics').textContent = '';
    $('#music-script-text').textContent = '';
    return;
  }
  const t = p.music_timing;
  $('#music-track-name').textContent = p.soundtrack?.original_name || 'Attach a WAV to review with music';
  $('#music-coverage').textContent = p.soundtrack
    ? `${musicClock(t.track_seconds)} track · ${musicClock(t.planned_seconds)} allocated · ${musicClock(t.gap_seconds)} without selected footage`
    : 'Choose Music & lyrics to attach your track.';
  $('#music-timing-summary').innerHTML = `<div class="music-metrics"><span><b>${musicClock(t.track_seconds)}</b> Track</span><span><b>${musicClock(t.planned_seconds)}</b> Shot allocations</span><span><b>${musicClock(t.covered_seconds)}</b> Selected footage</span><span><b>${musicClock(t.gap_seconds)}</b> Picture gaps</span></div><p class="fine">${musicClock(t.unallocated_seconds)} is beyond the current board.${t.overrun_seconds ? ` The board extends ${musicClock(t.overrun_seconds)} beyond the track.` : ''} Empty slots and ranges shorter than their allocation remain visible as gaps.${t.unmeasured_clips ? ` ${t.unmeasured_clips} selected clip(s) need their duration measured: open the clip and save its range.` : ''}</p>`;
  $('#music-import-note').textContent = p.source_import
    ? 'The storyboard is a working draft with provisional shot allocations. Song sections keep confirmed timing separately. Existing clip sound is excluded from preview.'
    : 'Source files and older soundtrack versions are retained. This is a review preview, not an encoded final cut.';
  const pieces = (p.lyrics || '').split(/(\[[^\]]+\])/);
  $('#music-lyrics').innerHTML = p.lyrics
    ? pieces.map(v => /^\[[^\]]+\]$/.test(v) ? `<h3>${esc(v.slice(1, -1))}</h3>` : `<p>${esc(v)}</p>`).join('')
    : '<p class="fine">No source lyrics attached.</p>';
  $('#music-source-script').hidden = !p.source_import?.screenplay_text;
  $('#music-script-text').textContent = p.source_import?.screenplay_text || '';
  renderMusicCues();
}

function renderMusicCues() {
  const p = project(), track = p.soundtrack;
  const cues = (p.music_cues || []).filter(c => c.track_sha256 === track?.sha256);
  $('#music-mark-cue').disabled = !track;
  $('#music-cue-list').innerHTML = cues.length ? cues.map(c => `<div class="music-cue"><button class="quiet" data-cue-time="${c.seconds}">${musicClock(c.seconds)}${c.end_seconds!=null?'–'+musicClock(c.end_seconds):''} · ${esc(c.label)}</button><button class="quiet" data-remove-cue="${c.id}" aria-label="Remove cue ${esc(c.label)}">Remove</button></div>`).join('') : '<p class="fine">No cues marked on this recording yet. Chorus and lyric times are still unconfirmed.</p>';
}

function bindClipPreview(video, asset) {
  const p = project();
  const silence = () => {
    if (p.music_video) {
      if (!video.muted) video.muted = true;
      if (video.volume !== 0) video.volume = 0;
    }
  };
  silence();
  video.addEventListener('volumechange', silence);
  video.addEventListener('play', () => {
    silence();
    const start = asset.in_seconds || 0, end = asset.out_seconds ?? video.duration;
    if (video.currentTime < start || video.currentTime >= end) video.currentTime = start;
  });
  video.addEventListener('timeupdate', () => {
    if (asset.out_seconds != null && video.currentTime >= asset.out_seconds) video.pause();
  });
  video.addEventListener('loadedmetadata', () => {
    if (previewId !== asset.id || stage !== 'clip') return;
    $('#clip-in').value = asset.in_seconds || 0;
    $('#clip-out').value = asset.out_seconds ?? video.duration;
    $('#clip-in').max = video.duration;
    $('#clip-out').max = video.duration;
    video.currentTime = asset.in_seconds || 0;
    $('#clip-range-note').textContent = `${musicClock(video.duration)} source · ${p.music_video ? 'clip audio off · ' : ''}Ranges leave the source file intact. A range shorter than the shot allocation leaves a visible gap.`;
  });
}

function showClipRange(asset) {
  $('#clip-range').hidden = stage !== 'clip' || !asset;
  const root = asset?.source_ref
    ? project().shots.find(s => s.id === asset.source_ref.shot_id)?.assets.find(a => a.id === asset.source_ref.asset_id)
    : asset;
  const review = root?.editorial_review;
  $('#asset-review-note').textContent = asset?.source_record
      ? `${asset.source_record.description || ''} ${asset.source_record.notes || ''} Review evidence: sampled frames; full motion review pending.${asset.source_ref ? ' This library use has its own range; trimming it leaves the source and other uses unchanged.' : ''}${review ? ` Latest source review: ${review.note}` : ''}`
    : review?.note || '';
  if (stage !== 'clip' || !asset) return;
  $('#clip-in').value = asset.in_seconds || 0;
  $('#clip-out').value = asset.out_seconds ?? asset.duration_seconds ?? '';
  $('#clip-range-note').textContent = 'Loading clip duration…';
  bindClipPreview($('#asset-preview video'), asset);
}

function stopMusicReview() {
  musicReviewActive = false;
  cancelAnimationFrame(musicFrame);
  musicScreenKey = '';
  $('#review-audio').pause();
  $('#review-music').hidden = true;
}

function startMusicReview() {
  const p = project();
  if (!p.music_video || !p.soundtrack) return false;
  $('#music-player').pause();
  stopReview();
  reviewMode = 'clips'; reviewList = [...p.shots]; reviewIndex = 0;
  const audio = $('#review-audio');
  audio.src = media(p.soundtrack);
  musicReviewActive = true;
  $('#review-music').hidden = false;
  $('#review-mode-label').textContent = 'MUSIC VIDEO · DRAFT PICTURE';
  $('#review-help').textContent = 'The song keeps its timing. Missing clips, short ranges and the unplanned tail stay visible. Clip audio is off. This browser preview is not frame-accurate final encoding.';
  $('#review-strip').innerHTML = p.shots.map((s, i) => `<button data-review="${i}">${String((i + 1) * 10).padStart(3, '0')} · ${esc(s.title)}</button>`).join('');
  $('#review-dialog').showModal();
  const begin = () => { if (musicReviewActive) { audio.currentTime = 0; audio.play().catch(e => { if (musicReviewActive && e.name !== 'AbortError') toast('Press play on the soundtrack to begin.'); }); } };
  if (audio.readyState) begin(); else audio.addEventListener('loadedmetadata', begin, {once:true});
  musicStep();
  return true;
}

function seekMusicReview(index) {
  const t = project().music_timing;
  $('#review-audio').currentTime = t.slots[index]?.start_seconds ?? t.planned_seconds;
  musicScreenKey = '';
}

function musicStep() {
  if (!musicReviewActive || !$('#review-dialog').open) return;
  const p = project(), audio = $('#review-audio'), time = audio.currentTime;
  const slot = p.music_timing.slots.find(s => time >= s.start_seconds && time < s.start_seconds + s.target_seconds);
  const s = slot ? p.shots.find(s => s.id === slot.shot_id) : null;
  const a = s && selected(s, 'clip');
  const offset = slot ? time - slot.start_seconds : 0;
  const start = a?.in_seconds || 0, end = a?.out_seconds ?? a?.duration_seconds;
  const inRange = a && (end == null || start + offset < end);
  const key = `${s?.id || 'tail'}:${a?.id || 'missing'}:${start}:${end}:${inRange}`;
  if (key !== musicScreenKey) {
    const old = $('#review-screen video'); if (old) old.pause();
    musicScreenKey = key;
    $('#review-heading').textContent = s?.title || 'Room for the next shots';
    $('#review-caption').innerHTML = s ? `<div class="eyebrow">${esc(s.scene || '')}</div><p>${esc(s.purpose)}</p>` : '<p>The soundtrack continues beyond the current board. Add shots before or after any card to build this section.</p>';
    if (inRange) {
      $('#review-screen').innerHTML = `<video playsinline muted preload="auto" ${a.poster_path ? `poster="${media({path:a.poster_path})}"` : ''} src="${media(a)}"></video>`;
    } else {
      $('#review-screen').innerHTML = `<div class="review-empty"><span class="eyebrow">PICTURE GAP</span><h3>${esc(s ? a ? 'Selected range ends here' : 'Choose or import a take' : 'Unplanned audio tail')}</h3><p>${s ? 'The song continues within this shot allocation.' : 'The music stays anchored while you extend the board.'}</p></div>`;
    }
    if (s) reviewIndex = p.shots.indexOf(s);
    $$('#review-strip button').forEach((b, i) => b.classList.toggle('active', !!s && i === reviewIndex));
  }
  $('#review-position').textContent = `${musicClock(time)} / ${musicClock(p.music_timing.track_seconds)}`;
  $('#music-review-status').textContent = s ? `${s.scene || 'Ungrouped'} · ${inRange ? 'Selected draft take' : 'Picture gap'} · clip audio off` : 'Unplanned audio tail · add shots to extend the board';
  $('#review-prev').disabled = !!s && reviewIndex === 0;
  $('#review-next').disabled = !s || reviewIndex >= p.shots.length - 1;
  const video = $('#review-screen video');
  if (video) {
    video.muted = true; video.volume = 0;
    if (video.readyState >= 1) {
      const target = Math.min(video.duration - .02, start + offset);
      if (Math.abs(video.currentTime - target) > .15) video.currentTime = Math.max(0, target);
      video.playbackRate = audio.playbackRate;
      if (audio.paused || audio.ended) video.pause();
      else if (video.paused && !video.dataset.starting) {
        video.dataset.starting = 'yes';
        video.play().catch(() => {}).finally(() => { delete video.dataset.starting; });
      }
    }
  }
  musicFrame = requestAnimationFrame(musicStep);
}

document.addEventListener('DOMContentLoaded', () => {
  document.addEventListener('click', safe(async e => {
    const button = e.target.closest('button'); if (!button) return;
    if (button.dataset.cueTime !== undefined) $('#music-player').currentTime = Number(button.dataset.cueTime);
    if (button.id === 'music-mark-cue' || button.id === 'music-save-timed-cue' || button.dataset.removeCue) {
      const p = project(); button.disabled = true;
      try {
        accept((await api('music-cue', {project:p.id, revision:p.revision,
          label:$('#music-cue-label').value, seconds:button.id === 'music-save-timed-cue' ? sectionTime($('#music-cue-start').value) : $('#music-player').currentTime,
          end_seconds:button.id === 'music-save-timed-cue' && $('#music-cue-end').value.trim() ? sectionTime($('#music-cue-end').value) : null,
          cue_id:button.dataset.removeCue, remove:!!button.dataset.removeCue})).project);
        if (!button.dataset.removeCue) $('#music-cue-label').value = '';
        toast(button.dataset.removeCue ? 'Song cue removed.' : 'Song cue marked on this recording.');
      } finally { button.disabled = false; }
    }
    if (button.id === 'attach-soundtrack') { $('#soundtrack-file').value = ''; $('#soundtrack-file').click(); }
    if (button.id === 'add-tail-shot') await insertShot(project().shots.at(-1).id, 'after');
    if (button.id === 'save-clip-range') {
      const video = $('#asset-preview video');
      if (!video || !Number.isFinite(video.duration)) throw new Error('Wait for the clip metadata to load.');
      const range = {asset:previewId, in_seconds:Number($('#clip-in').value),
        out_seconds:Number($('#clip-out').value), duration_seconds:video.duration};
      if (dirty) await saveShot();
      const p = project();
      accept((await api('clip-range', {project:p.id, shot:shotId, revision:p.revision, ...range})).project);
      await renderStage(); toast('Clip range saved. Source footage is unchanged.');
    }
  }));
  $('#soundtrack-file').addEventListener('change', safe(async e => {
    const file = e.target.files[0]; if (!file) return;
    if (!file.name.toLowerCase().endsWith('.wav') || file.size > 256 * 1024 * 1024) throw new Error('Use a PCM WAV smaller than 256 MiB.');
    const p = project();
    const query = new URLSearchParams({project:p.id, revision:p.revision, filename:file.name});
    const response = await fetch('/api/soundtrack?' + query, {method:'POST', headers:{'X-Storyboard-Token':state.token}, body:file});
    const data = await response.json(); if (!response.ok) throw new Error(data.error || 'Could not attach the soundtrack.');
    accept(data.project); toast('Soundtrack attached. Older versions are retained.');
  }));
});
