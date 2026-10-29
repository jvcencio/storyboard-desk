'use strict';
// Native SVG staging diagrams. These are not character designs or generated media.
function blockingFrame(s, aspect = '16:9') {
  if(aspect==='9:16')return portraitBlockingFrame(s);
  if (!s.blocking) return '';
  const key = String(s.id).replace(/[^a-zA-Z0-9_-]/g,'');
  const arrow = `arr-${key}`;
  const person = (x,y,label='J',scale=1,pose='stand') => `<g transform="translate(${x} ${y}) scale(${scale})" stroke="#46564f" stroke-width="3" fill="#f5f2e8"><circle cy="-62" r="15"/><path d="M-18-43 Q0-52 18-43 L24 5 H-24Z"/><path fill="none" d="${pose==='duck'?'M-16-34L-33-12M16-34L40-40':'M-16-33L-30 5M16-33L30 5'} M-12 6L-18 43M12 6L18 43"/><text x="0" y="-57" text-anchor="middle" fill="#46564f" stroke="none" font-size="14">${label}</text></g>`;
  const sign = (label='GATE',x=230,y=72) => `<rect x="${x}" y="${y}" width="150" height="32" rx="2" fill="#46564f"/><text x="${x+75}" y="${y+21}" text-anchor="middle" fill="#fffdf6" font-size="14">${label}</text>`;
  const path = d => `<path d="${d}" stroke="#b85934" stroke-width="3" stroke-dasharray="7 5" fill="none" marker-end="url(#${arrow})"/>`;
  let art='';
  const room='<path d="M35 237H605M70 55V237M570 55V237M70 55H570M70 237L170 175H470L570 237" fill="none" stroke="#c2c4b7" stroke-width="2"/>';
  if(s.blocking==='selfie'||s.blocking==='reaction') art=person(300,240,'J',2,s.blocking==='reaction'?'duck':'stand')+`<rect x="140" y="45" width="320" height="260" rx="18" stroke="#8b978a" fill="none"/>`+(s.blocking==='reaction'?path('M390 120Q430 110 440 80'):'');
  else if(s.blocking==='mirror') art=room+`<rect x="220" y="60" width="210" height="170" fill="#dfe4dc" stroke="#8b978a"/>`+person(325,206,'J',1.25)+`<path d="M200 245H450" stroke="#46564f" stroke-width="6"/>`;
  else if(s.blocking==='walkway') art=room+`<path d="M100 255L250 120H410L560 255Z" fill="#e3e3d6" stroke="#8b978a"/>`+person(290,178,'J',.95)+path('M350 240L350 155');
  else if(s.blocking==='sign') art=room+sign('DEPARTURES')+`<path d="M278 124H360M360 124L343 113M360 124L343 135" stroke="#b85934" stroke-width="5"/>`+person(170,228,'J',.75);
  else if(s.blocking==='pass') art=`${room}<rect x="230" y="185" width="150" height="63" transform="rotate(-12 300 210)" fill="#fffdf8" stroke="#b85934" stroke-width="3"/><path d="M250 207H350M250 220H325" stroke="#8b978a" stroke-width="3"/>${path('M430 165Q405 210 385 217')}`;
  else if(s.blocking==='passenger'||s.blocking==='help') art=room+sign()+person(215,210,'J',1)+person(390,210,'P',1)+`<rect x="430" y="210" width="35" height="45" rx="3" fill="#e2dfcf" stroke="#8b978a"/>`+(s.blocking==='help'?path('M245 173H350'):'');
  else if(s.blocking==='portal') art=room+`<path d="M350 245V68Q430 22 510 68V245Z" fill="#e3d3b5" stroke="#b85934" stroke-width="5"/><path d="M369 226V82Q430 48 490 82V226" fill="#fdf9e8" stroke="#d8c398" stroke-width="2"/>`+person(230,217,'J',1)+path('M260 225H408');
  else if(s.blocking==='gate') art=room+sign()+`<path d="M375 120V242M500 120V242M375 120H500" fill="none" stroke="#8b978a" stroke-width="4"/>`+person(250,220,'J',1)+path('M290 220H440');
  else if(s.blocking==='turbulence') art=`<g transform="rotate(-4 320 180)">${room}${sign()}${person(230,212,'J',1,'duck')}</g><path d="M70 275L90 263L110 275M500 275L520 263L540 275" fill="none" stroke="#b85934" stroke-width="4"/>`;
  else art=room+sign()+person(230,210,'J',1)+path('M270 232H460');
  return `<svg class="blocking-frame" viewBox="0 0 640 360" role="img" aria-label="Proposed blocking diagram for ${String(s.title).replace(/[&<>"']/g,'')}"><defs><marker id="${arrow}" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse"><path d="M0 0L10 5L0 10Z" fill="#b85934"/></marker></defs><rect width="640" height="360" fill="#eeebdf"/><text x="24" y="29" fill="#7d8578" font-size="12" letter-spacing="2">BEAT DIAGRAM · NOT A STARTER FRAME</text>${art}<path d="M24 326H616" stroke="#c2c4b7"/><text x="24" y="346" fill="#7d8578" font-size="11">J = Jorge · P = passenger · accent = intended motion / anomaly</text></svg>`;
}

function portraitBlockingFrame(s) {
  if(!s.blocking)return '';
  const key=String(s.id).replace(/[^a-zA-Z0-9_-]/g,'');
  const marker='portrait-arrow-'+key;
  const person=(x,y,label='J',scale=1)=>`<g transform="translate(${x} ${y}) scale(${scale})" stroke="#46564f" stroke-width="3" fill="#f5f2e8"><circle cy="-62" r="15"/><path d="M-18-43Q0-52 18-43L24 5H-24Z"/><path d="M-16-33L-30 5M16-33L30 5M-12 6L-18 43M12 6L18 43" fill="none"/><text y="-57" text-anchor="middle" fill="#46564f" stroke="none" font-size="14">${label}</text></g>`;
  const arrow=d=>`<path d="${d}" stroke="#b85934" stroke-width="3" stroke-dasharray="7 5" fill="none" marker-end="url(#${marker})"/>`;
  const room='<path d="M20 530H340M35 100V530M325 100V530M35 100H325M35 530L100 340H260L325 530" stroke="#bcc3b5" stroke-width="2" fill="none"/>';
  const sign=(label='GATE')=>`<rect x="85" y="150" width="190" height="34" rx="2" fill="#46564f"/><text x="180" y="172" text-anchor="middle" fill="#fffdf6" font-size="14">${label}</text>`;
  let art='';
  if(['selfie','reaction'].includes(s.blocking))art=`<rect x="55" y="100" width="250" height="440" rx="18" stroke="#8b978a" fill="none"/>${person(180,390,'J',2.1)}${s.blocking==='reaction'?arrow('M235 255Q285 230 285 185'):''}`;
  else if(s.blocking==='mirror')art=room+`<rect x="80" y="165" width="200" height="270" fill="#dfe4dc" stroke="#8b978a"/>${person(180,365,'J',1.5)}<path d="M65 463H295" stroke="#46564f" stroke-width="5"/>`;
  else if(s.blocking==='walkway')art=room+`<path d="M55 550L130 240H230L305 550Z" fill="#e0e1d4" stroke="#8b978a"/>${person(180,385,'J',1.15)}${arrow('M210 515L210 300')}`;
  else if(s.blocking==='sign')art=room+sign('DEPARTURES')+person(105,460,'J',1)+arrow('M150 215H240');
  else if(s.blocking==='pass')art=room+`<rect x="90" y="365" width="170" height="75" transform="rotate(-12 180 400)" fill="#fffdf8" stroke="#b85934" stroke-width="3"/><path d="M115 397H230M115 412H200" stroke="#8b978a" stroke-width="3"/>${arrow('M290 295Q275 345 255 373')}`;
  else if(['passenger','help'].includes(s.blocking))art=room+sign()+person(90,435,'J',1.15)+person(255,435,'P',1.15)+`<rect x="275" y="470" width="35" height="50" rx="3" fill="#e2dfcf" stroke="#8b978a"/>`+(s.blocking==='help'?arrow('M130 385H215'):'');
  else if(s.blocking==='portal')art=room+`<path d="M155 525V180Q235 115 310 180V525Z" fill="#e3d3b5" stroke="#b85934" stroke-width="4"/><path d="M173 502V196Q235 143 292 196V502" fill="#fdf9e8" stroke="#d8c398"/>`+person(85,430,'J',1.1)+arrow('M115 475H225');
  else if(s.blocking==='gate')art=room+sign()+`<path d="M175 240H290V525M175 240V525" fill="none" stroke="#8b978a" stroke-width="4"/>`+person(90,430,'J',1.1)+arrow('M125 475H245');
  else if(s.blocking==='turbulence')art=`<g transform="rotate(-3 180 320)">${room}${sign()}${person(120,430,'J',1.2)}</g><path d="M50 555L75 540L100 555M245 555L270 540L295 555" stroke="#b85934" stroke-width="4" fill="none"/>`;
  else art=room+sign()+person(120,430,'J',1.2)+arrow('M155 475H270');
  return `<svg class="blocking-frame" viewBox="0 0 360 640" role="img" aria-label="Portrait 9:16 blocking diagram for ${String(s.title).replace(/[&<>"']/g,'')}"><defs><marker id="${marker}" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto"><path d="M0 0L10 5L0 10Z" fill="#b85934"/></marker></defs><rect width="360" height="640" fill="#eeebdf"/><text x="20" y="30" fill="#7d8578" font-size="10" letter-spacing="1.2">9:16 PORTRAIT · BEAT DIAGRAM</text><text x="20" y="48" fill="#7d8578" font-size="10">Schematic only · not a starter frame</text>${art}<path d="M20 578H340" stroke="#c2c4b7"/><text x="20" y="600" fill="#7d8578" font-size="10">J = Jorge / camera position · P = passenger</text><text x="20" y="619" fill="#7d8578" font-size="10">Accent = intended motion or anomaly</text></svg>`;
}
