import {requestKey} from './request-key';
import {fileSHA256} from './file-sha256';
export type ImportKey='importPDF'|'pdfFile'|'importHelp'|'importLimit'|'importInvalid'|'importPreparing'|'importSending'|'importUncertain'|'checkImport'|'importRejected'|'history'|'requestExpired'|'reloadView'|'loading'|'failed';
type Options={t:(key:ImportKey)=>string;current:()=>boolean;load:(signal:AbortSignal)=>Promise<any>;send:(file:File,key:string,sha:string,signal:AbortSignal)=>Promise<any>;accepted:(job:any)=>void;history:()=>void;reload:()=>void;classify:(error:unknown)=>'expired'|'rejected'|'uncertain'};
export function importControls(options:Options){
 const t=options.t,element=document.createElement('form');element.className='pdf-import';
 const help=document.createElement('p');help.textContent=t('importHelp');
 const label=document.createElement('label');label.htmlFor='pdf-file';label.textContent=t('pdfFile');
 const file=document.createElement('input');file.type='file';file.id='pdf-file';file.accept='.pdf,application/pdf';
 const limit=document.createElement('p');limit.className='muted';limit.id='pdf-file-limit';file.setAttribute('aria-describedby',limit.id);
 const status=document.createElement('p');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
 const submit=document.createElement('button');submit.type='submit';submit.className='primary';
 const history=document.createElement('button');history.type='button';history.textContent=t('history');history.addEventListener('click',options.history);history.hidden=true;
 const reload=document.createElement('button');reload.type='button';reload.textContent=t('reloadView');reload.addEventListener('click',options.reload);reload.hidden=true;
 element.append(help,label,file,limit,status,submit,history,reload);
 const controller=new AbortController();let uploadController:AbortController|null=null;let disposed=false,maximum=0,pending=false,uncertain=false,expired=false,key='',sha='',selected:File|null=null;
 const active=()=>!disposed&&options.current();
 const valid=()=>!!selected&&selected.size>0&&selected.size<=maximum&&(selected.type==='application/pdf'||selected.name.toLowerCase().endsWith('.pdf'));
 const update=()=>{file.disabled=pending||uncertain||expired;submit.disabled=pending||expired||!valid();submit.textContent=t(uncertain?'checkImport':'importPDF');element.setAttribute('aria-busy',String(pending));history.hidden=!uncertain&&!expired;reload.hidden=!expired};
 const choose=(value:File|null)=>{selected=value;key='';sha='';status.textContent=selected&&!valid()?t('importInvalid'):'';update()};
 file.addEventListener('change',()=>{if(!pending&&!uncertain&&!expired)choose(file.files?.[0]??null)});
 element.addEventListener('submit',event=>{event.preventDefault();void send()});
 async function send(){
  if(!active()||pending||expired||!valid())return;pending=true;status.textContent=t(sha?'importSending':'importPreparing');update();let sending=false;
  try {
   if(!sha)sha=await fileSHA256(selected!,controller.signal);if(!active())return;
   if(!key)key=requestKey();status.textContent=t('importSending');sending=true;
   const upload=new AbortController();uploadController=upload;const timeout=setTimeout(()=>upload.abort(),60000);
   let job;try{job=await options.send(selected!,key,sha,upload.signal)}finally{clearTimeout(timeout);uploadController=null}
   if(!active())return;if(typeof job?.id!=='string'||!/^[A-Za-z0-9_-]{16,96}$/.test(job.id))throw new Error('Invalid import job');options.accepted(job);
  }catch(error){if(!active())return;delete element.dataset.automatic;const kind=sending?options.classify(error):'rejected';
   if(kind==='expired'){expired=true;status.textContent=t('requestExpired')}
   else if(kind==='rejected'&&!uncertain){key='';status.textContent=t('importRejected')}
   else{uncertain=true;status.textContent=t('importUncertain')}
  }finally{pending=false;if(active())update()}
 }
 async function start(initialFile?:File){if(initialFile)element.dataset.automatic='true';try{const result=await options.load(controller.signal);if(!active())return;const max=result?.limits?.job_max_bytes;if(!Number.isSafeInteger(max)||max<2)throw new Error('Invalid import limit');maximum=Math.min(max-1,1<<30);limit.textContent=t('importLimit').replace('{size}',new Intl.NumberFormat(document.documentElement.lang,{maximumFractionDigits:1}).format(maximum/(1024*1024)));status.textContent='';update();if(initialFile){const chosen=new DataTransfer();chosen.items.add(initialFile);file.files=chosen.files;choose(initialFile);if(valid())await send();else delete element.dataset.automatic}}catch{if(active()){delete element.dataset.automatic;status.textContent=t('failed');reload.hidden=false}}}
 status.textContent=t('loading');update();
 return {element,start,dispose(){disposed=true;controller.abort();uploadController?.abort()}};
}
