import {ocrControls} from './ocr';
import {requestKey} from './request-key';
import type {Key} from './messages';
type Options={doc:any;locale:string;t:(key:Key)=>string;api:(path:string,method?:string,body?:unknown,key?:string,revision?:number,signal?:AbortSignal)=>Promise<any>;refresh:()=>Promise<void>;stopJob:()=>void;current:()=>boolean};
const opaque=(id:unknown):id is string=>typeof id==='string'&&/^[A-Za-z0-9_-]{16,96}$/.test(id);
export function ocrResults(o:Options){
 const {doc,t,api}=o,base='/api/documents/'+doc.id;
 const element=document.createElement('section');element.className='ocr-results';
 const make=(tag:string,value:string)=>{const e=document.createElement(tag);e.textContent=value;return e};
 const button=(key:Key,action:()=>void)=>{const b=document.createElement('button');b.type='button';b.textContent=t(key);b.addEventListener('click',action);return b};
 const state=make('p','');state.setAttribute('role','status');
 let disposed=false,dialog:HTMLDialogElement|null=null,controls:ReturnType<typeof ocrControls>|null=null;
 const controller=new AbortController();const current=()=>!disposed&&o.current();
 const pendingKey='scanner.reocr.'+doc.id;
 const pending=()=>{try{return sessionStorage.getItem(pendingKey)!==null}catch{return true}};
 const clear=()=>{try{sessionStorage.removeItem(pendingKey)}catch{}};
 const refresh=async()=>{if(!current())return;await o.refresh();const heading=document.querySelector<HTMLElement>('[data-reocr-heading]'),action=document.querySelector<HTMLButtonElement>('[data-reocr-action]');(action&&!action.disabled?action:heading)?.focus()};
 const fail=()=>{state.textContent=t('reocrUnknown')};
 // app.ts owns all job polling and terminal transitions, including automatic saving.
 const cancelKey=requestKey();
 const cancel=button('reocrCancel',()=>void(async()=>{cancel.disabled=true;o.stopJob();try{await api('/api/jobs/'+doc.active_job_id+'/cancel','POST',undefined,cancelKey,undefined,controller.signal);if(current()){clear();await refresh()}}catch{if(current()){fail();check.hidden=false}}finally{if(current())cancel.disabled=false}})());cancel.hidden=true;
 const dispositionKey='scanner.reocr.disposition.'+doc.id;
 const readDisposition=()=>{try{const raw=sessionStorage.getItem(dispositionKey);if(!raw)return null;const value=JSON.parse(raw);if(!opaque(value.id)||!['adopt','discard'].includes(value.action)||!opaque(value.key)||!Number.isSafeInteger(value.revision))throw new Error('Invalid intent');return value}catch{return {invalid:true}}};
 const clearDisposition=()=>{try{sessionStorage.removeItem(dispositionKey)}catch{}};
 const check=button('reocrCheck',()=>void(async()=>{
  check.disabled=true;
  try{const intent=readDisposition();if(intent){if(intent.invalid)throw new Error('Invalid intent');const result=await api(base+'/ocr-results/'+intent.id,'GET',undefined,undefined,undefined,controller.signal);if(result.id!==intent.id||result.state!==(intent.action==='adopt'?'adopted':'discarded'))throw new Error('Uncertain disposition');clearDisposition()}await refresh()}catch{if(current())fail()}finally{if(current())check.disabled=false}
 })());
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
   try{const job=await api(base+'/reocr','POST',command,key,doc.revision,AbortSignal.any([controller.signal,AbortSignal.timeout(15000)]));if(!current())return;if(!opaque(job?.id)||job.document_id!==doc.id||job.kind!=='render')throw new Error('Invalid recognition job');clear();dialog?.close();await refresh()}
   catch(err){if(current()){const code=(err as {status?:number}).status;if(code&&code>=400&&code<500){clear();hint.textContent=t(code===412?'changed':code===410?'requestExpired':'failed');start.disabled=false}else{hint.textContent=t('reocrUnknown');start.disabled=true}check.hidden=false}}
  })());submit.className='primary';submit.disabled=true;
  controls=ocrControls(signal=>api('/api/capabilities','GET',undefined,undefined,undefined,signal),{enable:t('searchablePDF'),model:t('ocrModel'),languages:t('ocrLanguages'),quality:t('ocrQuality'),chooseModel:t('chooseModel'),chooseQuality:t('chooseQuality'),loading:t('loading'),unavailable:t('ocrUnavailable'),retry:t('retry'),external:t('ocrExternal'),presets:{auto:t('automatic'),standard:t('standardQuality'),fast:t('fastQuality'),accurate:t('accurateQuality')}},o.locale,()=>{submit.disabled=sending||!controls?.enabled()||!controls.valid()});
  const actions=document.createElement('div');actions.className='output-actions';actions.append(close,submit);dialog.append(title,hint,controls.element,actions);element.append(dialog);
  dialog.addEventListener('close',()=>{controls?.dispose();controls=null;dialog?.remove();dialog=null;if(current())start.focus()},{once:true});dialog.showModal();controls.start();
 });start.dataset.reocrAction='true';start.disabled=!!doc.active_job_id||pending();check.hidden=!pending();element.append(start,cancel,check,state);
 if(pending())state.textContent=t('reocrUnknown');
 if(doc.active_job_id)cancel.hidden=false;
 const summaries=Array.isArray(doc.ocr_results)?doc.ocr_results:[];
 const candidate=summaries.find((r:any)=>r.state==='candidate');
 if(candidate||readDisposition())start.disabled=true;
 if(readDisposition()){check.hidden=false;fail()}
 const previous=summaries.find((r:any)=>r.state==='adopted');
 if(previous&&opaque(previous.id)&&doc.output?.revision!==doc.revision){const link=document.createElement('a');link.className='download-link';link.href=base+'/ocr-results/'+previous.id+'/output';link.textContent=t('savePDF');element.prepend(link)}
 let comparisonAbort:AbortController|null=null,comparisonVersion=0,expiryTimer:ReturnType<typeof setTimeout>|undefined;
 const expiryHandlers=new Set<()=>void>(),expires=Date.parse(doc.expires_at);
 const expire=()=>{const remaining=expires-Date.now();if(remaining>0)expiryTimer=setTimeout(expire,Math.min(remaining,2147483647));else for(const clear of expiryHandlers)clear()};expire();
 if(summaries.length){
  if(summaries.length>2||summaries.some((r:any)=>!opaque(r.id)||!['candidate','adopted'].includes(r.state)||!Number.isSafeInteger(r.input_revision)))throw new Error('Invalid result summaries');
  const title=make('h2',t('reocrCompare'));title.tabIndex=-1;title.dataset.reocrHeading='true';element.append(title);
  const label=make('label',t('page'));const select=document.createElement('select');label.append(select);
  for(const [index,p] of doc.pages.entries()){if(!opaque(p.id))throw new Error('Invalid page');const option=document.createElement('option');option.value=p.id;option.textContent=t('page')+' '+(index+1);select.append(option)}
  const pageKey='scanner.reocr.page.'+doc.id;try{const saved=sessionStorage.getItem(pageKey);if(doc.pages.some((p:any)=>p.id===saved))select.value=saved!}catch{}
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
   expiryHandlers.clear();panels.replaceChildren();
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
     let overlay:SVGRectElement|null=null,clearSuggestion=()=>{};
     if(artifacts[kind]===prefix+kind){
      const w=p.pixel_width,h=p.pixel_height,sourceRotation=p.pdf_raster?.SourceRotation??0;if(![0,90,180,270].includes(sourceRotation))throw new Error('Invalid PDF rotation');const rotation=(p.rotation+sourceRotation)%360;
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox',`0 0 ${rotation%180?h:w} ${rotation%180?w:h}`);svg.setAttribute('role','img');svg.setAttribute('aria-label',t('preview'));svg.classList.add('ocr-source-preview');
      const group=document.createElementNS(svg.namespaceURI,'g');group.setAttribute('transform',rotation===90?`translate(${h} 0) rotate(90)`:rotation===180?`translate(${w} ${h}) rotate(180)`:rotation===270?`translate(0 ${w}) rotate(270)`:'');
      const image=document.createElementNS(svg.namespaceURI,'image');image.setAttribute('href',prefix+kind);image.setAttribute('width',String(w));image.setAttribute('height',String(h));
      const rect=document.createElementNS(svg.namespaceURI,'rect') as SVGRectElement;rect.classList.add('ocr-highlight');rect.setAttribute('visibility','hidden');overlay=rect;
      image.addEventListener('error',()=>{overlay=null;clearSuggestion();svg.replaceWith(make('p',t('reocrPreviewUnavailable')))},{once:true});group.append(image,rect);svg.append(group);panel.append(svg);
     }else panel.append(make('p',t('reocrPreviewUnavailable')));
     if(p.transcript_unavailable||!artifacts.text){panel.append(make('p',t('reocrTextUnavailable')));return}
     const transcript=await artifact(artifacts.text,prefix+'text',signal);if(!current()||version!==comparisonVersion)return;
     let positions:any=null;
     if(artifacts.positions){try{positions=JSON.parse(await artifact(artifacts.positions,prefix+'positions',signal));if(positions.page_id!==pageID||positions.width!==p.pixel_width||positions.height!==p.pixel_height||!Array.isArray(positions.spans)||positions.spans.length>10000||positions.spans.some((s:any)=>typeof s.text!=='string'||s.text.length>65536||!s.box||![s.box.x,s.box.y,s.box.width,s.box.height].every(Number.isFinite)||s.box.x<0||s.box.y<0||s.box.width<=0||s.box.height<=0||s.box.x+s.box.width>p.pixel_width||s.box.y+s.box.height>p.pixel_height))positions=null}catch{positions=null}}
     if(!current()||version!==comparisonVersion)return;
     if(!positions){panel.append(make('p',t('reocrPositionsUnavailable')),make('pre',transcript));return}
     const spans=document.createElement('ol');spans.className='ocr-spans';spans.setAttribute('aria-label',t('reocrOrder'));
     const optional=document.createElement('div'),notice=make('p','');notice.setAttribute('role','status');
     let selected=-1,generation=0;
     const available=()=>current()&&version===comparisonVersion&&!signal.aborted&&!!overlay&&Date.now()<Date.parse(doc.expires_at);
     clearSuggestion=()=>{generation++;const focused=optional.contains(document.activeElement);optional.replaceChildren();notice.textContent=t('structureUnavailable');if(focused)spans.querySelectorAll('button')[selected]?.focus()};expiryHandlers.add(clearSuggestion);
     const sourceIdentity=(r:any)=>{const pages=r.pages?.filter((v:any)=>v.page_id===pageID);return r.id===result.id&&r.input_revision===result.input_revision&&r.state===result.state&&pages?.length===1&&JSON.stringify(pages[0])===JSON.stringify(p)};
     const validate=async(index:number)=>{
      const deadline=AbortSignal.any([signal,AbortSignal.timeout(15000)]);
      if(!available()||!sourceIdentity(await api(resultBase,'GET',undefined,undefined,undefined,deadline)))throw new Error('Unavailable source');
      const [text,raw]=await Promise.all([artifact(artifacts.text,prefix+'text',deadline),artifact(artifacts.positions,prefix+'positions',deadline)]),fresh=JSON.parse(raw);
      if(!available()||text!==transcript||fresh.page_id!==pageID||fresh.width!==positions.width||fresh.height!==positions.height||JSON.stringify(fresh.spans?.[index])!==JSON.stringify(positions.spans[index]))throw new Error('Changed source');
     };
     const create=button('structureCreate',()=>void(async()=>{
      const index=selected,token=++generation,span=positions.spans[index];create.disabled=true;
      try{
       await validate(index);if(token!==generation)return;
       const name=Array.from(span.text.normalize('NFC').replace(/[\u0000-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069/\\:*?"<>|]/gu,' ').replace(/\s+/gu,' ').trim().replace(/^[. ]+|[. ]+$/gu,'')).slice(0,64).join('');
       if(!name)throw new Error('Empty suggestion');
       const generated=document.createElement('div');generated.dataset.structureSuggestion='true';generated.setAttribute('role','region');generated.setAttribute('aria-label',t('structureGenerated'));
       generated.append(make('h4',t('structureGenerated')),make('p',t('structureHelp')),make('p',name+'.pdf'));
       const reference=make('p',`${result.id} · ${t('reocrRevision')} ${result.input_revision} · ${t('page')} ${doc.pages.findIndex((v:any)=>v.id===pageID)+1} (${pageID}) · ${t('structureItem')} ${index+1} · ${p.effective_sha256}`);reference.className='ocr-source';
       const source=button('structureSource',()=>void(async()=>{source.disabled=true;try{await validate(index);if(token!==generation)return;const control=spans.querySelectorAll('button')[index];control.focus();control.click()}catch{if(current()&&version===comparisonVersion){clearSuggestion();spans.querySelectorAll('button')[index]?.focus()}}finally{source.disabled=false}})());
       generated.append(reference,source);optional.replaceChildren(create,generated);notice.textContent='';
      }catch{if(current()&&version===comparisonVersion&&token===generation){clearSuggestion();spans.querySelectorAll('button')[index]?.focus()}}finally{create.disabled=false}
     })());
     for(const [index,span] of positions.spans.entries()){
      const row=document.createElement('li');const control=document.createElement('button');control.type='button';control.textContent=`${index+1}. ${span.text}`;control.setAttribute('aria-pressed','false');
      const selectSpan=()=>{for(const button of spans.querySelectorAll('button'))button.setAttribute('aria-pressed',String(button===control));if(overlay){for(const key of ['x','y','width','height'])overlay.setAttribute(key,String(span.box[key]));overlay.setAttribute('visibility','visible')}if(selected!==index){selected=index;generation++;optional.replaceChildren();notice.textContent='';if(available()&&span.text.trim()&&transcript.includes(span.text))optional.append(create);else notice.textContent=t('structureUnavailable')}};
      control.addEventListener('click',selectSpan);control.addEventListener('focus',selectSpan);row.append(control);spans.append(row);
     }panel.append(spans,optional,notice);
    }catch{if(current()&&version===comparisonVersion)panel.replaceChildren(make('h3',t(summary.state==='candidate'?'reocrCandidate':'reocrPrevious')),make('p',t('reocrTextUnavailable')))}
   }));
  }
  select.addEventListener('change',()=>{try{sessionStorage.setItem(pageKey,select.value)}catch{}void compare()});void compare();
  if(candidate){
   const actions=document.createElement('div');actions.className='output-actions';
   const adopt=button('reocrAdopt',()=>void disposeCandidate('adopt'));
   const discard=button('reocrDiscard',()=>void disposeCandidate('discard'));
   adopt.disabled=!!doc.active_job_id||candidate.input_revision!==doc.revision||!!readDisposition();discard.disabled=!!doc.active_job_id||!!readDisposition();
   async function disposeCandidate(action:'adopt'|'discard'){
    if(readDisposition())return;adopt.disabled=discard.disabled=true;
    const intent={id:candidate.id,action,key:requestKey(),revision:doc.revision};
    try{sessionStorage.setItem(dispositionKey,JSON.stringify(intent))}catch{state.textContent=t('failed');return}
    try{await api(base+'/ocr-results/'+intent.id+'/'+action,'POST',{},intent.key,intent.revision,AbortSignal.any([controller.signal,AbortSignal.timeout(15000)]));if(!current())return;clearDisposition();clear();await refresh()}
    catch(err){if(!current())return;const code=(err as {status?:number}).status;
     if(code&&code>=400&&code<500){clearDisposition();state.textContent=t(code===412||code===409?'changed':code===410?'requestExpired':'failed');check.hidden=false}
     else{fail();check.hidden=false}
    }
   }
   actions.append(discard,adopt);element.append(actions);
  }
 }
 return {element,dispose:()=>{disposed=true;controller.abort();comparisonAbort?.abort();clearTimeout(expiryTimer);expiryHandlers.clear();controls?.dispose();dialog?.remove()}};
}
