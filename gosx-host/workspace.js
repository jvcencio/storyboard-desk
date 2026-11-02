'use strict';
// Browser-only affordances. Project commands and validation run in Go.
const dirtyForms = new Set();
const saveStatus = document.getElementById('save-status');
document.addEventListener('input', event => {
  const form = event.target.closest('form[data-dirty]');
  if (form) { dirtyForms.add(form); if (saveStatus) saveStatus.textContent = 'Unsaved changes'; }
});
document.addEventListener('change', event => {
  const form = event.target.closest('form[data-dirty]');
  if (form) dirtyForms.add(form);
});
document.addEventListener('submit', event => {
  if (event.target.matches('[data-download-form]')) return;
  const others = [...dirtyForms].filter(form => form !== event.target);
  if (others.length && !window.confirm('Other forms have unsaved changes. Continue and discard those edits?')) { event.preventDefault(); return; }
  dirtyForms.clear();
});
document.addEventListener('click', event => {
  const anchor = event.target.closest('a[href]');
  if (event.ctrlKey || event.metaKey || event.button > 0) return;
  if (anchor && dirtyForms.size && !anchor.getAttribute('href').startsWith('#') && !anchor.hasAttribute('download')) {
    if (!window.confirm('Discard unsaved edits and leave this view?')) event.preventDefault();
    else dirtyForms.clear();
  }
});
window.addEventListener('beforeunload', event => { if (dirtyForms.size) { event.preventDefault(); event.returnValue = ''; } });
document.querySelectorAll('[data-copy]').forEach(button => button.addEventListener('click', async () => {
  const source = document.getElementById(button.dataset.copy);
  try { await navigator.clipboard.writeText(source.value); button.textContent = 'Copied'; }
  catch { source.focus(); source.select(); button.textContent = 'Select text and copy'; }
}));
document.querySelectorAll('[data-json-file]').forEach(input => input.addEventListener('change', async () => {
  const status = input.parentElement.querySelector('[role=status]');
  const file = input.files[0];
  if (!file) return;
  try {
    if (file.size > 1048576) throw new Error('Choose a JSON file smaller than 1 MiB.');
    const value = await file.text();
    JSON.parse(value);
    const target = input.form.elements[input.dataset.jsonFile];
    target.value = value;
    target.dispatchEvent(new Event('input', {bubbles:true}));
    status.textContent = 'Loaded into the form. Review the text before applying it.';
  } catch (error) { status.textContent = error.message; }
}));
document.querySelectorAll('a[href="#new-project"]').forEach(anchor => anchor.addEventListener('click', () => { const target = document.getElementById('new-project'); target.open = true; target.querySelector('input[name="title"]').focus(); }));
document.querySelectorAll('[data-asset] video').forEach(video => video.addEventListener('loadedmetadata', () => {
  if (!Number.isFinite(video.duration) || video.duration <= 0) return;
  const card = video.closest('[data-asset]');
  card.querySelectorAll('input[name="measured"]').forEach(input => { input.value = video.duration; });
  card.querySelectorAll('input[name="end"]').forEach(input => { if (!input.value) input.value = video.duration; });
}));
document.querySelectorAll('[data-review]').forEach(review => {
  const shots = [...review.querySelectorAll('[data-review-shot]')];
  const audio = review.querySelector('[data-review-audio]');
  const status = review.querySelector('[data-review-status]');
  let index = 0, playing = false, started = 0, elapsed = 0, frame;
  shots.forEach(shot => shot.querySelector('video')?.addEventListener('loadedmetadata', () => {
    const video = shot.querySelector('video');
    if (Number(shot.dataset.available) <= 0 && Number.isFinite(video.duration)) shot.dataset.available = Math.max(0, video.duration - Number(shot.dataset.start));
  }));
  const pause = () => { playing = false; cancelAnimationFrame(frame); shots.forEach(shot => shot.querySelector('video')?.pause()); audio?.pause(); };
  const show = () => { pause(); elapsed = 0; shots.forEach((shot, i) => { shot.hidden = i !== index; }); const shot = shots[index]; const video = shot?.querySelector('video'); if (video) video.currentTime = Number(shot.dataset.start); if (audio) audio.currentTime = Number(shot?.dataset.songStart || 0); status.textContent = `Shot ${index + 1} of ${shots.length}`; };
  const play = async () => { const shot = shots[index]; if (!shot || shot.dataset.kind === 'gap') { status.textContent = 'Choose a take for this shot before continuing.'; return; } playing = true; try { await shot.querySelector('video')?.play(); } catch { pause(); status.textContent = 'This source could not play. Review it in the Media Library.'; return; } if (!playing || shots[index] !== shot) return; started = performance.now() - elapsed * 1000; audio?.play().catch(() => {}); frame = requestAnimationFrame(step); };
  const step = now => { if (!playing) return; const shot = shots[index]; elapsed = (now - started) / 1000; const target = Number(shot.dataset.duration); const available = Number(shot.dataset.available); if (elapsed >= Math.min(target, available)) { if (available + .05 < target) { pause(); status.textContent = `This take leaves ${(target - available).toFixed(2)} seconds of missing coverage. Choose another range or advance manually.`; return; } if (index + 1 < shots.length) { index++; show(); play(); } else { pause(); status.textContent = 'Sequence complete'; } return; } frame = requestAnimationFrame(step); };
  review.querySelector('[data-review-play]').addEventListener('click', () => { if (playing) pause(); else play(); });
  review.querySelector('[data-review-prev]').addEventListener('click', () => { index = Math.max(0, index - 1); show(); });
  review.querySelector('[data-review-next]').addEventListener('click', () => { index = Math.min(shots.length - 1, index + 1); show(); });
  window.addEventListener('pagehide', pause); show();
});
document.querySelectorAll('[data-cloud-form]').forEach(form => {
  const kind = form.elements.kind, model = form.elements.model, prompt = form.elements.prompt;
  let previous = form.querySelector('[data-default-image]').value;
  const sync = () => {
    for (const option of model.options) option.hidden = option.dataset.kind !== kind.value;
    if (model.selectedOptions[0]?.hidden) model.value = [...model.options].find(option => !option.hidden)?.value || '';
    const next = form.querySelector(kind.value === 'video' ? '[data-default-video]' : '[data-default-image]').value;
    if (!prompt.value || prompt.value === previous) prompt.value = next;
    previous = next;
    form.querySelectorAll('input[name="reference_ids"]').forEach(input => { input.disabled = kind.value === 'video'; });
  };
  kind.addEventListener('change', sync); sync();
});
