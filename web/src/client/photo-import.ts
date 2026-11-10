import {requestKey} from './request-key';
import {fileSHA256} from './file-sha256';
import type {Key} from './messages';
type Identity={key:string;sha256:string;input_bytes:number;media_type:'image/jpeg'|'image/png';phase:'uncertain'};
const storageKey='scanner.photo.import';
export function photoImportControls(options:{t:(key:Key)=>string;current:()=>boolean;load:(signal:AbortSignal)=>Promise<any>;send:(file:File,key:string,sha:string,signal:AbortSignal)=>Promise<any>;reconcile:(identity:Identity,signal:AbortSignal)=>Promise<any>;accepted:(job:any)=>void;classify:(error:unknown)=>'expired'|'rejected'|'uncertain'}){
 const {t}=options,element=document.createElement('section');element.className='photo-import';
 const actions=document.createElement('div');actions.className='photo-import-actions';
 const status=document.createElement('p');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
 const controller=new AbortController();let upload:AbortController|null=null,disposed=false,pending=false,maximum=0,identity:Identity|null=null;
 const active=()=>!disposed&&options.current();
 const persist=()=>{try{if(identity)sessionStorage.setItem(storageKey,JSON.stringify(identity));else sessionStorage.removeItem(storageKey)}catch{}};
 try{const value=JSON.parse(sessionStorage.getItem(storageKey)||'null');if(value&&/^[A-Za-z0-9_-]{16,512}$/.test(value.key)&&/^[a-f0-9]{64}$/.test(value.sha256)&&Number.isSafeInteger(value.input_bytes)&&value.input_bytes>0&&value.input_bytes<=32*1024*1024&&['image/jpeg','image/png'].includes(value.media_type)&&value.phase==='uncertain')identity={key:value.key,sha256:value.sha256,input_bytes:value.input_bytes,media_type:value.media_type,phase:'uncertain'}}catch{}
 const inputs:HTMLInputElement[]=[],buttons:HTMLButtonElement[]=[];
 const check=document.createElement('button');check.type='button';check.textContent=t('checkImport');check.addEventListener('click',()=>void receive());
 const update=()=>{for(const b of buttons)b.disabled=pending||!!identity||maximum===0;for(const i of inputs)i.disabled=pending||!!identity||maximum===0;check.hidden=!identity;check.disabled=pending;element.setAttribute('aria-busy',String(pending))};
 for(const camera of [true,false]){const input=document.createElement('input');input.type='file';input.accept='image/jpeg,image/png';input.hidden=true;if(camera)input.setAttribute('capture','environment');else input.dataset.photoPicker='true';const button=document.createElement('button');button.type='button';button.textContent=t(camera?'camera':'photoPicker');button.addEventListener('click',()=>input.click());input.addEventListener('cancel',()=>{if(active()){status.textContent=t('photoChooserCancelled');button.focus()}});input.addEventListener('change',()=>{const file=input.files?.[0];input.value='';if(file)void receive(file)});inputs.push(input);buttons.push(button);actions.append(button,input)}
 element.append(actions,status,check);
 async function receive(file?:File){
  if(!active()||pending)return;
  if(file){if(!['image/jpeg','image/png'].includes(file.type)){status.textContent=t(/heic|heif/i.test(file.type+file.name)?'photoHEIC':'photoUnsupported');return}if(file.size<1||file.size>maximum){status.textContent=t('photoLimit');return}}
  if(!file&&!identity)return;pending=true;status.textContent=t(file?'importPreparing':'importUncertain');update();let sent=false;
  try{
   if(file){const sha=await fileSHA256(file,controller.signal);if(!active())return;identity={key:requestKey(),sha256:sha,input_bytes:file.size,media_type:file.type as Identity['media_type'],phase:'uncertain'};persist()}
   upload=new AbortController();const timeout=setTimeout(()=>upload?.abort(),60000);sent=true;let job;
   try{job=file?await options.send(file,identity!.key,identity!.sha256,upload.signal):await options.reconcile(identity!,upload.signal)}finally{clearTimeout(timeout);upload=null}
   if(!active())return;if(typeof job?.id!=='string'||!/^[A-Za-z0-9_-]{16,96}$/.test(job.id))throw new Error('Invalid photo receipt');identity=null;persist();options.accepted(job);
  }catch(error){if(!active())return;const kind=sent?options.classify(error):'rejected';if(kind==='uncertain')status.textContent=t('importUncertain');else{identity=null;persist();status.textContent=t(kind==='expired'?'requestExpired':'photoRejected')}}finally{pending=false;if(active())update()}
 }
 async function start(){try{const result=await options.load(controller.signal);if(!active())return;const max=result?.limits?.job_max_bytes;if(!Number.isSafeInteger(max)||max<2)throw new Error('Invalid limit');maximum=Math.min(max-1,32*1024*1024);status.textContent=identity?t('importUncertain'):t('photoHelp');update()}catch{if(active())status.textContent=t('failed')}}
 update();return {element,start,dispose(){disposed=true;controller.abort();upload?.abort()}};
}
