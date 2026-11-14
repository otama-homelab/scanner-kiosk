import {requestKey} from './request-key';
import {fileSHA256} from './file-sha256';
import {isPDFFile} from './import';
import type {CaptureIntent} from './capture';
import type {Key} from './messages';
export type PhotoImportIntent={purpose:'photo';photo_format:'jpeg'|'png'};
type Identity={key:string;sha256:string;input_bytes:number;media_type:'image/jpeg'|'image/png';phase:'uncertain'}&({purpose?:undefined;photo_format?:undefined}|PhotoImportIntent);
const storageKey='scanner.photo.import';
export function photoImportControls(options:{openPDF:(file:File)=>void;getIntent:()=>CaptureIntent;restoreIntent:(intent:CaptureIntent)=>void;lockIntent:(locked:boolean)=>void;t:(key:Key)=>string;current:()=>boolean;load:(signal:AbortSignal)=>Promise<any>;send:(file:File,key:string,sha:string,signal:AbortSignal,intent?:PhotoImportIntent)=>Promise<any>;reconcile:(identity:Identity,signal:AbortSignal)=>Promise<any>;accepted:(job:any)=>void;classify:(error:unknown)=>'expired'|'rejected'|'uncertain'}){
 const {t}=options,element=document.createElement('div');element.className='photo-import-feedback';
 const actions=document.createElement('div');actions.className='photo-import-actions';
 const status=document.createElement('p');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
 const controller=new AbortController();let upload:AbortController|null=null,disposed=false,pending=false,maximum=0,identity:Identity|null=null;
 const active=()=>!disposed&&options.current();
 const persist=()=>{try{if(identity)sessionStorage.setItem(storageKey,JSON.stringify(identity));else sessionStorage.removeItem(storageKey)}catch{}};
 try{const value=JSON.parse(sessionStorage.getItem(storageKey)||'null');if(value&&/^[A-Za-z0-9_-]{16,512}$/.test(value.key)&&/^[a-f0-9]{64}$/.test(value.sha256)&&Number.isSafeInteger(value.input_bytes)&&value.input_bytes>0&&value.input_bytes<=32*1024*1024&&['image/jpeg','image/png'].includes(value.media_type)&&value.phase==='uncertain'){
  const base={key:value.key,sha256:value.sha256,input_bytes:value.input_bytes,media_type:value.media_type,phase:'uncertain' as const};
  if((value.purpose===undefined||value.purpose==='document')&&value.photo_format===undefined)identity=base;
  else if(value.purpose==='photo'&&(value.photo_format===undefined||['jpeg','png'].includes(value.photo_format)))identity={...base,purpose:'photo',photo_format:value.photo_format||'jpeg'};
 }}catch{}
 if(identity)options.restoreIntent({purpose:identity.purpose==='photo'?'photo':'document',photo_format:identity.photo_format||'jpeg'});
 const inputs:HTMLInputElement[]=[],buttons:HTMLButtonElement[]=[];
 const check=document.createElement('button');check.type='button';check.textContent=t('checkImport');check.addEventListener('click',()=>void receive());
 const update=()=>{for(const b of buttons)b.disabled=pending||!!identity||maximum===0;for(const i of inputs)i.disabled=pending||!!identity||maximum===0;options.lockIntent(pending||!!identity);check.hidden=!identity;check.disabled=pending;element.hidden=!status.textContent&&!identity;element.setAttribute('aria-busy',String(pending))};
 for(const camera of [false,true]){const input=document.createElement('input');input.type='file';input.accept=camera?'image/jpeg,image/png':'.pdf,application/pdf,image/jpeg,image/png';input.hidden=true;if(camera)input.setAttribute('capture','environment');else input.dataset.photoPicker='true';const button=document.createElement('button');button.type='button';button.textContent=t(camera?'camera':'photoPicker');button.addEventListener('click',()=>input.click());input.addEventListener('cancel',()=>{if(active()){status.textContent=t('photoChooserCancelled');update();button.focus()}});input.addEventListener('change',()=>{const file=input.files?.[0];input.value='';if(file)void receive(file,!camera)});inputs.push(input);buttons.push(button);actions.append(button,input)}
 element.append(status,check);
 async function receive(file?:File,allowPDF=false){
  if(!active()||pending||file&&identity)return;
  if(file&&allowPDF&&isPDFFile(file)){options.openPDF(file);return}
  if(file){if(!['image/jpeg','image/png'].includes(file.type)){status.textContent=t(/heic|heif/i.test(file.type+file.name)?'photoHEIC':'photoUnsupported');update();return}if(file.size<1||file.size>maximum){status.textContent=t('photoLimit');update();return}}
  if(!file&&!identity)return;const choice=options.getIntent();const selectedIntent=choice.purpose==='photo'?{purpose:'photo' as const,photo_format:choice.photo_format}:{};pending=true;status.textContent=t(file?'importPreparing':'importUncertain');update();let sent=false;
  try{
   if(file){const sha=await fileSHA256(file,controller.signal);if(!active())return;identity={key:requestKey(),sha256:sha,input_bytes:file.size,media_type:file.type as Identity['media_type'],phase:'uncertain',...selectedIntent};persist()}
   upload=new AbortController();const timeout=setTimeout(()=>upload?.abort(),60000);sent=true;let job;
   try{const intent=identity!.purpose==='photo'?{purpose:'photo' as const,photo_format:identity!.photo_format!}:undefined;job=file?await options.send(file,identity!.key,identity!.sha256,upload.signal,intent):await options.reconcile(identity!,upload.signal)}finally{clearTimeout(timeout);upload=null}
   if(!active())return;if(typeof job?.id!=='string'||!/^[A-Za-z0-9_-]{16,96}$/.test(job.id))throw new Error('Invalid photo receipt');identity=null;persist();options.accepted(job);
  }catch(error){if(!active())return;const kind=sent?options.classify(error):'rejected';if(kind==='uncertain')status.textContent=t('importUncertain');else{identity=null;persist();status.textContent=t(kind==='expired'?'requestExpired':'photoRejected')}}finally{pending=false;if(active())update()}
 }
 async function start(){try{const result=await options.load(controller.signal);if(!active())return;const max=result?.limits?.job_max_bytes;if(!Number.isSafeInteger(max)||max<2)throw new Error('Invalid limit');maximum=Math.min(max-1,32*1024*1024);status.textContent=identity?t('importUncertain'):'';update()}catch{if(active()){status.textContent=t('failed');update()}}}
 update();return {element,actions,start,dispose(){disposed=true;controller.abort();upload?.abort()}};
}
