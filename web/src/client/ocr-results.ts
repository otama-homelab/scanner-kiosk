import {ocrControls} from './ocr';
import {requestKey} from './request-key';
import type {Key} from './messages';
type Options={doc:any;locale:string;t:(key:Key)=>string;api:(path:string,method?:string,body?:unknown,key?:string,revision?:number,signal?:AbortSignal)=>Promise<any>;refresh:()=>Promise<void>;current:()=>boolean};
const opaque=(id:unknown):id is string=>typeof id==='string'&&/^[A-Za-z0-9_-]{16,96}$/.test(id);
const active=new Set(['queued','running','cancelling','reconciling']);
export function ocrResults(o:Options){
 const {doc,t,api}=o,base='/api/documents/'+doc.id;
 const element=document.createElement('section');element.className='ocr-results';
 const make=(tag:string,value:string)=>{const e=document.createElement(tag);e.textContent=value;return e};
 const button=(key:Key,action:()=>void)=>{const b=document.createElement('button');b.type='button';b.textContent=t(key);b.addEventListener('click',action);return b};
 const state=make('p','');state.setAttribute('role','status');
 let disposed=false,dialog:HTMLDialogElement|null=null,controls:ReturnType<typeof ocrControls>|null=null,timer:ReturnType<typeof setTimeout>|undefined;
 const controller=new AbortController();const current=()=>!disposed&&o.current();
 const pendingKey='scanner.reocr.'+doc.id;
 const pending=()=>{try{return sessionStorage.getItem(pendingKey)!==null}catch{return true}};
 const clear=()=>{try{sessionStorage.removeItem(pendingKey)}catch{}};
 const refresh=async()=>{await o.refresh();document.querySelector<HTMLButtonElement>('[data-reocr-action]')?.focus()};
 const fail=()=>{state.textContent=t('reocrUnknown')};
 async function watch(id:string){
  if(!opaque(id))return;
  try{const job=await api('/api/jobs/'+id,'GET',undefined,undefined,undefined,controller.signal);if(!current())return;
   if(job.id!==id||job.document_id!==doc.id||job.kind!=='render')throw new Error('Invalid recognition job');
   state.textContent=job.state;
   if(active.has(job.state))timer=setTimeout(()=>void watch(id),750);
   else{clear();cancel.hidden=true;if(job.state==='succeeded')await refresh();else start.disabled=false}
  }catch{if(current())fail()}
 }
 const cancel=button('reocrCancel',()=>void(async()=>{cancel.disabled=true;try{await api('/api/jobs/'+doc.active_job_id+'/cancel','POST',undefined,requestKey(),undefined,controller.signal);if(current())void watch(doc.active_job_id)}catch{if(current())fail()}finally{if(current())cancel.disabled=false}})());cancel.hidden=true;
 const check=button('reocrCheck',()=>void refresh());
 const start=button('reocr',()=>{
  if(dialog||doc.active_job_id||pending())return;
  dialog=document.createElement('dialog');dialog.className='output-settings';dialog.setAttribute('aria-labelledby','reocr-title');
  const title=make('h2',t('reocr'));title.id='reocr-title';const hint=make('p',t('reocrHelp'));hint.setAttribute('role','status');
  const close=button('close',()=>dialog?.close());let sending=false;
  const submit=button('reocrStart',()=>void(async()=>{
   if(sending||!controls?.enabled()||!controls.valid())return;
   const command={format:'pdf',ocr:controls.command()},key=requestKey();
   // Persist uncertainty before sending. A reload may reconcile, never invent a new key.
   try{sessionStorage.setItem(pendingKey,JSON.stringify({key,revision:doc.revision}))}catch{hint.textContent=t('failed');return}
   sending=true;submit.disabled=true;controls.lock(true);
   try{const job=await api(base+'/reocr','POST',command,key,doc.revision,controller.signal);if(!current())return;if(!opaque(job?.id)||job.document_id!==doc.id||job.kind!=='render')throw new Error('Invalid recognition job');clear();dialog?.close();await refresh()}
   catch(err){if(current()){hint.textContent=t('reocrUnknown');check.hidden=false;start.disabled=true}}
  })());submit.className='primary';submit.disabled=true;
  controls=ocrControls(signal=>api('/api/capabilities','GET',undefined,undefined,undefined,signal),{enable:t('searchablePDF'),model:t('ocrModel'),languages:t('ocrLanguages'),quality:t('ocrQuality'),chooseModel:t('chooseModel'),chooseQuality:t('chooseQuality'),loading:t('loading'),unavailable:t('ocrUnavailable'),retry:t('retry'),external:t('ocrExternal'),presets:{auto:t('automatic'),standard:t('standardQuality'),fast:t('fastQuality'),accurate:t('accurateQuality')}},o.locale,()=>{submit.disabled=sending||!controls?.enabled()||!controls.valid()});
  const actions=document.createElement('div');actions.className='output-actions';actions.append(close,submit);dialog.append(title,hint,controls.element,actions);element.append(dialog);
  dialog.addEventListener('close',()=>{controls?.dispose();controls=null;dialog?.remove();dialog=null;if(current())start.focus()},{once:true});dialog.showModal();controls.start();
 });start.dataset.reocrAction='true';start.disabled=!!doc.active_job_id||pending();check.hidden=!pending();element.append(start,cancel,check,state);
 if(pending())state.textContent=t('reocrUnknown');
 if(doc.active_job_id){cancel.hidden=false;void watch(doc.active_job_id)}
 const summaries=Array.isArray(doc.ocr_results)?doc.ocr_results:[];
 let comparisonAbort:AbortController|null=null,comparisonVersion=0;
 if(summaries.length){
  if(summaries.length>2||summaries.some((r:any)=>!opaque(r.id)||!['candidate','adopted'].includes(r.state)||!Number.isSafeInteger(r.input_revision)))throw new Error('Invalid result summaries');
  const title=make('h2',t('reocrCompare'));title.tabIndex=-1;title.dataset.reocrHeading='true';element.append(title);
  const label=make('label',t('page'));const select=document.createElement('select');label.append(select);
  for(const [index,p] of doc.pages.entries()){if(!opaque(p.id))throw new Error('Invalid page');const option=document.createElement('option');option.value=p.id;option.textContent=t('page')+' '+(index+1);select.append(option)}
  const panels=document.createElement('div');panels.className='ocr-comparison';element.append(label,panels);
  async function artifact(url:string,expected:string,signal:AbortSignal){
   if(url!==expected)throw new Error('Mismatched result artifact');
   const response=await fetch(expected,{credentials:'same-origin',cache:'no-store',redirect:'error',signal});
   if(!response.ok||!response.body)throw new Error('Unavailable result');
   const reader=response.body.getReader(),chunks:Uint8Array[]=[];let size=0;
   try{while(true){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>2*1024*1024)throw new Error('Result too large');chunks.push(value)}}finally{await reader.cancel()}
   const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length}return new TextDecoder('utf-8',{fatal:true}).decode(bytes);
  }
  async function compare(){
   comparisonAbort?.abort();comparisonAbort=new AbortController();const signal=comparisonAbort.signal,version=++comparisonVersion,pageID=select.value;
   panels.replaceChildren();
   await Promise.all(summaries.map(async(summary:any)=>{
    const panel=document.createElement('section');panel.className='ocr-result-panel';panel.dataset.resultId=summary.id;panel.append(make('h3',t(summary.state==='candidate'?'reocrCandidate':'reocrPrevious')),make('p',t('loading')));panels.append(panel);
    try{
     const resultBase=base+'/ocr-results/'+summary.id;
     const result=await api(resultBase,'GET',undefined,undefined,undefined,signal);
     if(!current()||version!==comparisonVersion)return;
     if(result.id!==summary.id||result.input_revision!==summary.input_revision||!Array.isArray(result.pages)||result.pages.length>1000||!result.settings?.ocr?.model_id||!result.settings.ocr.model_version)throw new Error('Invalid result identity');
     const matches=result.pages.filter((p:any)=>p.page_id===pageID);if(matches.length!==1)throw new Error('Missing result page');const p=matches[0];
     if(!/^[a-f0-9]{64}$/.test(p.original_sha256)||!/^[a-f0-9]{64}$/.test(p.effective_sha256)||![0,90,180,270].includes(p.rotation)||!Number.isSafeInteger(p.pixel_width)||!Number.isSafeInteger(p.pixel_height)||p.pixel_width<=0||p.pixel_height<=0||p.pixel_width*p.pixel_height>40000000)throw new Error('Invalid source');
     panel.replaceChildren(make('h3',t(summary.state==='candidate'?'reocrCandidate':'reocrPrevious')));
     panel.append(make('p',`${t('reocrRevision')} ${result.input_revision} · ${result.settings.ocr.provider_id} / ${result.settings.ocr.model_id} / ${result.settings.ocr.model_version}`));
     const provenance=make('p',`${t('reocrSource')}: ${p.effective_sha256}`);provenance.className='ocr-source';panel.append(provenance,make('p',t(p.pdf_text_origin==='retained-source'?'reocrRetained':'reocrRecognized')));
     const prefix=resultBase+'/pages/'+pageID+'/',artifacts=p.artifacts??{},kind=p.source_type==='pdf'?'preview':'effective';
     let overlay:SVGRectElement|null=null;
     if(artifacts[kind]===prefix+kind){
      const w=p.pixel_width,h=p.pixel_height,sourceRotation=p.pdf_raster?.SourceRotation??0;if(![0,90,180,270].includes(sourceRotation))throw new Error('Invalid PDF rotation');const rotation=(p.rotation+sourceRotation)%360;
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox',`0 0 ${rotation%180?h:w} ${rotation%180?w:h}`);svg.setAttribute('role','img');svg.setAttribute('aria-label',t('preview'));svg.classList.add('ocr-source-preview');
      const group=document.createElementNS(svg.namespaceURI,'g');group.setAttribute('transform',rotation===90?`translate(${h} 0) rotate(90)`:rotation===180?`translate(${w} ${h}) rotate(180)`:rotation===270?`translate(0 ${w}) rotate(270)`:'');
      const image=document.createElementNS(svg.namespaceURI,'image');image.setAttribute('href',prefix+kind);image.setAttribute('width',String(w));image.setAttribute('height',String(h));
      const rect=document.createElementNS(svg.namespaceURI,'rect') as SVGRectElement;rect.classList.add('ocr-highlight');rect.setAttribute('visibility','hidden');overlay=rect;
      image.addEventListener('error',()=>{overlay=null;svg.replaceWith(make('p',t('reocrPreviewUnavailable')))},{once:true});group.append(image,rect);svg.append(group);panel.append(svg);
     }else panel.append(make('p',t('reocrPreviewUnavailable')));
     if(p.transcript_unavailable||!artifacts.text){panel.append(make('p',t('reocrTextUnavailable')));return}
     const transcript=await artifact(artifacts.text,prefix+'text',signal);if(!current()||version!==comparisonVersion)return;
     let positions:any=null;
     if(artifacts.positions){try{positions=JSON.parse(await artifact(artifacts.positions,prefix+'positions',signal));if(positions.page_id!==pageID||positions.width!==p.pixel_width||positions.height!==p.pixel_height||!Array.isArray(positions.spans)||positions.spans.length>10000||positions.spans.some((s:any)=>typeof s.text!=='string'||s.text.length>65536||!s.box||![s.box.x,s.box.y,s.box.width,s.box.height].every(Number.isFinite)||s.box.x<0||s.box.y<0||s.box.width<=0||s.box.height<=0||s.box.x+s.box.width>p.pixel_width||s.box.y+s.box.height>p.pixel_height))positions=null}catch{positions=null}}
     if(!current()||version!==comparisonVersion)return;
     if(!positions){panel.append(make('p',t('reocrPositionsUnavailable')),make('pre',transcript));return}
     const spans=document.createElement('ol');spans.className='ocr-spans';spans.setAttribute('aria-label',t('reocrOrder'));
     for(const [index,span] of positions.spans.entries()){
      const row=document.createElement('li');const control=document.createElement('button');control.type='button';control.textContent=`${index+1}. ${span.text}`;control.setAttribute('aria-pressed','false');
      const selectSpan=()=>{for(const button of spans.querySelectorAll('button'))button.setAttribute('aria-pressed',String(button===control));if(overlay){for(const key of ['x','y','width','height'])overlay.setAttribute(key,String(span.box[key]));overlay.setAttribute('visibility','visible')}};
      control.addEventListener('click',selectSpan);control.addEventListener('focus',selectSpan);row.append(control);spans.append(row);
     }panel.append(spans);
    }catch{if(current()&&version===comparisonVersion)panel.replaceChildren(make('h3',t(summary.state==='candidate'?'reocrCandidate':'reocrPrevious')),make('p',t('reocrTextUnavailable')))}
   }));
  }
  select.addEventListener('change',()=>void compare());void compare();
 }
 return {element,dispose:()=>{disposed=true;controller.abort();comparisonAbort?.abort();if(timer)clearTimeout(timer);controls?.dispose();dialog?.remove()}};
}
