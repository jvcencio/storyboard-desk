'use strict';
let showBinnedTakes = false;
const localDrawingChoices = new Map();
function drawingChoice() {
  const key=projectId+':'+shotId;
  if(!localDrawingChoices.has(key))localDrawingChoices.set(key,{mode:'text',image_id:'',strength:.4});
  return localDrawingChoices.get(key);
}
function drawingImages() {
  const p=project();
  return [...(p.visual_references||[]).map(r=>({...r,label:'Reference · '+r.title})),
    ...p.shots.flatMap(s=>s.assets.filter(a=>['sketch','keyframe'].includes(a.kind)&&!a.binned_at)
      .map(a=>({...a,label:s.title+' · '+a.kind+' take '+a.take})))];
}
function renderLocalDrawing() {
  const panel=$('#local-drawing');panel.hidden=stage!=='sketch';if(panel.hidden)return;
  const choice=drawingChoice(),images=drawingImages();
  if(choice.image_id&&!images.some(a=>a.id===choice.image_id))choice.image_id='';
  const input=images.find(a=>a.id===choice.image_id);
  panel.innerHTML=`<div class="eyebrow">LOCAL LAYOUT STUDY</div>
    <div class="two-fields"><label>Drawing mode<select id="drawing-mode"><option value="text" ${choice.mode==='text'?'selected':''}>Text → rough layout</option><option value="image" ${choice.mode==='image'?'selected':''}>Image → variation</option></select></label>
    <label ${choice.mode==='image'?'':'hidden'}>Change strength<input id="drawing-strength" type="number" min="0.1" max="0.85" step="0.05" value="${choice.strength}"></label></div>
    <div ${choice.mode==='image'?'':'hidden'}><label>Starting image<select id="drawing-image"><option value="">Choose a saved image…</option>${images.map(a=>`<option value="${esc(a.id)}" ${a.id===choice.image_id?'selected':''}>${esc(a.label)}</option>`).join('')}</select></label>
    ${input?`<img class="drawing-input" src="${media(input)}" alt="Selected local drawing input"><p class="fine">${esc(input.label)}</p>`:''}
    <p class="fine">This varies the starting canvas. It does not lock a character’s identity. The input is scaled and center-cropped to ${esc(project().aspect)}; a turnaround sheet can reproduce multiple figures or lose its edges. Prefer a scene image composed for this frame.</p></div>
    <p class="fine">Use a short visual prompt for one starting frame. Local layouts and image variations need review for character and style consistency.</p>
    <button type="button" class="quiet" id="edit-local-prompt">Edit drawing prompt ↗</button>`;
}
function localDrawingRequest() {
  const choice=drawingChoice();
  if(choice.mode==='image'&&!choice.image_id)throw Error('Choose a starting image for the variation.');
  return {mode:choice.mode,image_id:choice.mode==='image'?choice.image_id:null,strength:choice.strength,revision:project().revision};
}
document.addEventListener('change',e=>{
  if(!['drawing-mode','drawing-image','drawing-strength'].includes(e.target.id))return;
  const choice=drawingChoice();
  if(e.target.id==='drawing-mode')choice.mode=e.target.value;
  if(e.target.id==='drawing-image')choice.image_id=e.target.value;
  if(e.target.id==='drawing-strength')choice.strength=Number(e.target.value);
  renderLocalDrawing();
});
document.addEventListener('click',e=>{
  if(e.target.closest('#edit-local-prompt')){
    const field=$('#shot-form [name=local_prompt]');field.focus();field.scrollIntoView({block:'center',behavior:'smooth'});
  }
});
