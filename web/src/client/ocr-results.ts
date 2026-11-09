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
 return {element,dispose:()=>{disposed=true;controller.abort();if(timer)clearTimeout(timer);controls?.dispose();dialog?.remove()}};
}
