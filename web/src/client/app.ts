import {iconButton} from './icons';
import {pdfPreview as createPDFPreview} from './pdf-preview';
import {requestKey,observeRequestTime} from './request-key';
import {deletionDialog} from './deletion';
import {ocrControls} from './ocr';
import {ocrResults} from './ocr-results';
import {passwordLogin} from './login';
import {captureControls} from './capture';
import {correctionControl,photoCorrectionControl} from './correction';
import {importControls} from './import';
import {photoImportControls,type PhotoImportIntent} from './photo-import';
import {deliveryControl} from './delivery';
import {messages,type Key} from './messages';
let locale:'en'|'ja'='en';try{if(localStorage.getItem('scanner.locale')==='ja')locale='ja'}catch{}
const t=(key:Key)=>messages[locale][key];
const main=document.querySelector<HTMLElement>('#main')!;
const status=document.querySelector<HTMLElement>('#status')!;
const appVersion=document.querySelector<HTMLElement>('#app-version')!;
const skipLink=document.querySelector<HTMLAnchorElement>('#skip-link')!;
skipLink.addEventListener('click',event=>{event.preventDefault();main.focus()});
const button=(label:Key,action:()=>void)=>{const b=document.createElement('button');b.type='button';b.textContent=t(label);b.addEventListener('click',action);return b};
const text=(tag:string,value:string)=>{const e=document.createElement(tag);e.textContent=value;return e};
function imageFallback(image:HTMLImageElement,previewURL:string){let fallback=false;image.addEventListener('error',()=>{if(!fallback){fallback=true;image.src=previewURL}else{const notice=text('p',t('photoPreviewUnavailable'));notice.className='preview-unavailable';notice.setAttribute('role','status');image.replaceWith(notice)}})}
function documentName(doc:any){return new Intl.DateTimeFormat(locale,{dateStyle:'medium',timeStyle:'short'}).format(new Date(doc.created_at))}
const opaque=(id:unknown):id is string=>typeof id==='string'&&/^[A-Za-z0-9_-]{16,96}$/.test(id);
class APIError extends Error{constructor(readonly status:number,readonly retryAfter=2,readonly code=''){super('Request failed')}}
async function api(path:string,method='GET',body?:unknown,key?:string,revision?:number,signal?:AbortSignal){
 const headers:Record<string,string>={};if(key)headers['Idempotency-Key']=key;if(revision)headers['If-Match']=`"${revision}"`;if(body!==undefined)headers['Content-Type']='application/json';
 return responseData(await fetch(path,{method,headers,body:body===undefined?undefined:JSON.stringify(body),credentials:'same-origin',cache:'no-store',redirect:'error',signal}));
}
async function responseData(response:Response){
 observeRequestTime(response.headers.get('Date'));
 if(!response.ok){let code='';try{const value=await response.json();if(typeof value?.code==='string'&&/^[a-z_]{1,64}$/.test(value.code))code=value.code}catch{}const delay=Number(response.headers.get('Retry-After'));const retryAfter=Number.isFinite(delay)&&delay>0?Math.min(60,Math.ceil(delay)):2;throw new APIError(response.status,retryAfter,code)}const data=await response.text();return data?JSON.parse(data):null;
}
let session:{authenticated:boolean;principal_kind:string;authenticate_type:string}|null=null;
let generation=0;
let disposeDialog:(()=>void)|null=null;
let disposeLogin:(()=>void)|null=null;
let disposeCapture:(()=>void)|null=null;
let disposeImport:(()=>void)|null=null;
let disposePhotoImport:(()=>void)|null=null;
let jobViewKey='';
let saveJob='';
let saveDocument='';
let disposePreviews:(()=>void)[]=[];
let nextNotice:Key|undefined;
let immediateScan:string|null=null;
let historyLayout:'list'|'grid'='list';try{if(localStorage.getItem('scanner.history.layout')==='grid')historyLayout='grid'}catch{}
let jobTimer:ReturnType<typeof setTimeout>|undefined;
let jobAbort:AbortController|null=null;
function stopJob(){if(jobTimer)clearTimeout(jobTimer);jobTimer=undefined;jobAbort?.abort();jobAbort=null}
function error(err:unknown){
 if(err instanceof APIError&&err.status===429){status.textContent=t('rateLimited').replace('{seconds}',new Intl.NumberFormat(locale).format(err.retryAfter));return}
 const expired=err instanceof APIError&&err.status===410;status.textContent=t(expired?'requestExpired':err instanceof APIError&&err.status===412?'changed':'failed');if(expired)status.append(button('reloadView',()=>void render()))
}
async function mutation(b:HTMLButtonElement,action:()=>Promise<void>){b.disabled=true;status.textContent='';try{await action()}catch(err){error(err)}finally{b.disabled=false}}
function localize(){document.documentElement.lang=locale;skipLink.textContent=t('skipContent');document.querySelector('nav')!.setAttribute('aria-label',t('navigation'));document.querySelector('.theme-switch')!.setAttribute('aria-label',t('appearance'));document.querySelector<HTMLSelectElement>('#locale')!.value=locale;document.querySelector('#language-label')!.textContent=t('language');for(const name of ['home','history','logout'] as const)document.querySelector('#'+name)!.textContent=t(name==='logout'&&session?.principal_kind==='guest'?'end':name);for(const name of ['light','dark','system'] as const)document.querySelector(`[data-theme-choice="${name}"]`)!.setAttribute('aria-label',t(name))}
function heading(key:Key,root:HTMLElement=main){document.title=`${t(key)} · Scanner`;root.append(text('h1',t(key)))}
function focusHeading(){const title=main.querySelector('h1');if(title){title.tabIndex=-1;title.focus()}}
function navigate(hash:string,notice?:Key){nextNotice=notice;if(location.hash===hash)void render();else location.hash=hash}
let boot:Promise<any>|null=null;
let renderedHash:string|null=null;
async function bootstrap(){
 if(!boot)boot=(async()=>{
  let current=await api('/api/session');
  if(current?.reset_required===true){
   try{await api('/auth/session/reset','POST',undefined,requestKey())}catch(err){if(!(err instanceof APIError&&err.status===409))throw err}
   current=await api('/api/session');if(current?.reset_required===true)throw new Error('Session recovery incomplete');
  }
  if(current?.authenticate_type==='none'&&!current.authenticated)current=await api('/auth/guest','POST',undefined,requestKey());
  return current;
 })().finally(()=>boot=null);return boot;
}
async function render(){
 disposePhotoImport?.();disposePhotoImport=null;
 const requestedScan=immediateScan;immediateScan=null;const hash=location.hash;if(saveJob&&hash!=='#/document/'+saveDocument&&hash!=='#/job/'+saveJob)saveJob='';const moveFocus=renderedHash!==null&&renderedHash!==hash;const focused=document.activeElement;
 for(const dispose of disposePreviews)dispose();disposePreviews=[];disposeImport?.();disposeImport=null;disposeCapture?.();disposeCapture=null;disposeDialog?.();disposeDialog=null;disposeLogin?.();disposeLogin=null;stopJob();jobViewKey='';const version=++generation;status.textContent=nextNotice?t(nextNotice):'';nextNotice=undefined;main.replaceChildren(text('p',t('loading')));main.setAttribute('aria-busy','true');
 try{
  const current=await bootstrap();if(version!==generation)return;if(!['guest','oidc','simple','none'].includes(current?.authenticate_type))throw new Error('Invalid authentication type');session=current;appVersion.textContent=typeof current.version==='string'?current.version:'';appVersion.hidden=!appVersion.textContent;document.querySelector<HTMLElement>('#history')!.hidden=!session?.authenticated;document.querySelector<HTMLElement>('#logout')!.hidden=!session?.authenticated;localize();main.replaceChildren();
  if(!session?.authenticated&&session?.authenticate_type==='simple'){
   document.title=`${t('signin')} · Scanner`;
   disposeLogin=passwordLogin({root:main,t,current:()=>version===generation,status:key=>status.textContent=key?t(key):'',
    classify:err=>err instanceof APIError&&err.code==='authentication_rejected'?{message:'invalidCredentials',reset:false}:err instanceof APIError&&[401,409,410].includes(err.status)?{message:'loginExpired',reset:true}:err instanceof APIError&&err.status===429?{message:'loginLimited',reset:false}:{message:'failed',reset:false},
    submit:(credentials,signal)=>api('/auth/simple/login','POST',credentials,requestKey(),undefined,signal),
    reset:async signal=>{try{return await api('/auth/logout','POST',undefined,requestKey(),undefined,signal)}catch(err){if(!(err instanceof APIError&&err.status===401&&err.code==='session_required'))throw err}},done:render});return
  }
  if(!session?.authenticated){const entry=document.createElement('section');entry.className='auth-card session-entry';const icon=document.createElement('img');icon.src='/logo.svg';icon.alt='';icon.width=icon.height=48;entry.append(icon);heading(session?.authenticate_type==='guest'?'session':'signin',entry);entry.append(text('p',t(session?.authenticate_type==='guest'?'guestHelp':'signinHelp')));main.append(entry);const key=requestKey();const login=button(session?.authenticate_type==='guest'?'start':'signin',()=>void mutation(login,async()=>{
   try{if(session?.authenticate_type==='guest'){await api('/auth/guest','POST',undefined,key);await render()}
   else{const result=await api('/auth/oidc/start','POST',undefined,key);const url=new URL(result.authorization_url);if(url.protocol!=='https:'||url.username||url.password)throw new Error('Invalid target');location.assign(url.href)}}catch(err){if(err instanceof APIError&&[401,409,410].includes(err.status))reset.hidden=false;throw err}
  }));login.className='primary';entry.append(login);
  const reset=button('reset',()=>void mutation(reset,async()=>{await api(session?.authenticate_type==='guest'?'/auth/guest/end':'/auth/logout','POST',undefined,requestKey());await render()}));reset.hidden=true;entry.append(reset);return}
  const route=location.hash.slice(1);
  if(route==='/import'){heading('importPDF');main.append(button('back',()=>navigate('')));mountImport(version);return}
  if(route.startsWith('/job/')){const id=route.slice('/job/'.length);if(!opaque(id))throw new Error('Invalid job');await loadJob(id,version);return}
  const scanURL=new URL(route,location.origin);
  if(route.startsWith('/document/')&&scanURL.pathname.endsWith('/scan')){
   const id=scanURL.pathname.slice('/document/'.length,-'/scan'.length);
   const entries=[...scanURL.searchParams.entries()];
   if(!opaque(id)||entries.length>1||entries.some(([mode,target])=>!['before','replace'].includes(mode)||!opaque(target)))throw new Error('Invalid scan target');
   const [mode,target]=entries[0]||[];
   const doc=await api('/api/documents/'+id);if(version!==generation)return;
   const active=activeJob(doc);if(active){navigate('#/job/'+active);return}
   if(!Number.isSafeInteger(doc.revision)||!Array.isArray(doc.pages)||!['review','ready','failed'].includes(doc.state)||target&&!doc.pages.some((page:any)=>page.id===target)||doc.purpose==='photo'&&doc.pages.length&&mode!=='replace')throw new Error('Invalid scan state');
   const placement=mode==='before'?{insert_before_page_id:target}:mode==='replace'?{replace_page_id:target}:undefined;
   const header=document.createElement('div');header.className='capture-header';const back=button('back',()=>navigate('#/document/'+id));back.className='back-link';header.append(back);heading(mode==='before'?'insertScan':mode==='replace'?'replaceScan':doc.purpose==='photo'?'photo':'scanNext',header);main.append(header);
   if(mode==='replace')main.append(text('p',t('replacementNotice')));
   mountCapture(version,doc,placement,requestedScan===id&&!placement);return
  }
  if(route.startsWith('/document/')){const id=route.slice('/document/'.length);if(!opaque(id))throw new Error('Invalid document');const value=await api('/api/documents/'+id);if(version!==generation)return;showDocument(value);return}
  if(route==='/history'){const header=document.createElement('div');header.className='history-header';heading('history',header);const views=document.createElement('div');views.className='history-layout';views.setAttribute('role','group');views.setAttribute('aria-label',t('historyView'));const list=document.createElement('div');list.className='document-list';list.dataset.layout=historyLayout;for(const layout of ['list','grid'] as const){const key=layout==='list'?'listView':'gridView';const control=iconButton(button(key,()=>{historyLayout=layout;list.dataset.layout=layout;for(const b of Array.from(views.querySelectorAll('button')))b.setAttribute('aria-pressed',String(b.dataset.layout===layout));try{localStorage.setItem('scanner.history.layout',layout)}catch{}}),key);control.dataset.layout=layout;control.setAttribute('aria-pressed',String(layout===historyLayout));views.append(control)}header.append(views);main.append(header,list);await history(list,null,version);return}
  const homeHeading=document.createElement('div');homeHeading.className='home-heading';heading('home',homeHeading);const file=document.createElement('input');file.type='file';file.accept='.pdf,application/pdf';file.hidden=true;file.addEventListener('change',()=>{const selected=file.files?.[0];file.value='';if(!selected||version!==generation)return;disposeCapture?.();disposeCapture=null;const view=++generation;main.replaceChildren();heading('job');main.append(button('back',()=>navigate('')));mountImport(view,selected)});homeHeading.append(button('importPDF',()=>file.click()),file);main.append(homeHeading);const capture=mountCapture(version);const photo=mountPhotoImport(version);capture.sourceActions.append(photo.actions);
 }catch(err){if(version===generation){main.replaceChildren(button('retry',()=>void render()));error(err)}}finally{
  if(version===generation){main.setAttribute('aria-busy','false');if(location.hash===hash){renderedHash=hash;if(moveFocus&&(document.activeElement===document.body||document.activeElement===focused&&focused instanceof Element&&focused.closest('nav,.brand')))focusHeading()}}
 }
}
function mountCapture(view:number,doc?:any,placement?:{insert_before_page_id?:string;replace_page_id?:string},immediate=false){const controls=captureControls({t,current:()=>view===generation,read:signal=>api('/api/scanners','GET',undefined,undefined,undefined,signal),submitLabel:placement?.replace_page_id?'replaceScan':placement?.insert_before_page_id?'insertScan':doc?.purpose==='photo'?'scan':undefined,submit:(body,key,signal)=>api(doc?'/api/documents/'+doc.id+'/scans':'/api/documents','POST',placement?{...(body as object),...placement}:body,key,doc?.revision,signal),accepted:job=>navigate('#/job/'+job.id),history:()=>navigate('#/history'),expired:err=>err instanceof APIError&&err.status===410,reload:()=>void render(),rejected:err=>err instanceof APIError&&(err.status>=400&&err.status<500||err.status===503&&err.code==='scanner_unavailable'),purpose:doc?.purpose,initialCapture:doc?.pages?.findLast((page:any)=>page.capture)?.capture,photoFormat:doc?.photo_format==='png'?'png':'jpeg'});main.append(controls.element);disposeCapture=controls.dispose;if(immediate){controls.element.hidden=true;status.textContent=t('loading')}void controls.start(immediate).catch(err=>{if(view===generation)error(err)}).finally(()=>{if(view===generation){controls.element.hidden=false;if(immediate&&status.textContent===t('loading'))status.textContent=''}});return controls}

function mountImport(view:number,file?:File){const controls=importControls({t,current:()=>view===generation,load:signal=>api('/api/scanners','GET',undefined,undefined,undefined,signal),send:(file,key,sha,signal)=>responseDataFromUpload(file,key,sha,signal),accepted:job=>navigate('#/job/'+job.id),history:()=>navigate('#/history'),reload:()=>void render(),classify:err=>err instanceof APIError&&err.status===410?'expired':err instanceof APIError&&err.status>=400&&err.status<500&&![401,404].includes(err.status)?'rejected':'uncertain'});main.append(controls.element);disposeImport=controls.dispose;void controls.start(file)}

function mountPhotoImport(view:number){const controls=photoImportControls({t,current:()=>view===generation,load:signal=>api('/api/scanners','GET',undefined,undefined,undefined,signal),send:responseDataFromUpload,reconcile:({key,phase,...identity},signal)=>api('/api/documents/import/reconcile','POST',identity,key,undefined,signal),accepted:job=>navigate('#/job/'+job.id),classify:err=>err instanceof APIError&&[404,410].includes(err.status)?'expired':err instanceof APIError&&err.status>=400&&err.status<500&&err.status!==401?'rejected':'uncertain'});main.append(controls.element);disposePhotoImport=controls.dispose;void controls.start();return controls}

async function responseDataFromUpload(file:File,key:string,sha:string,signal:AbortSignal,intent?:PhotoImportIntent){return responseData(await fetch('/api/documents/import',{method:'POST',headers:{'Content-Type':file.type==='image/jpeg'||file.type==='image/png'?file.type:'application/pdf','Idempotency-Key':key,'X-Content-SHA256':sha,...intent?{'X-Document-Purpose':intent.purpose,'X-Photo-Format':intent.photo_format}:{}},body:file,credentials:'same-origin',cache:'no-store',redirect:'error',signal}))}

function documentDeleteButton(doc:any,label:'deleteDocument'|'deletePhoto'|'deletePage'='deleteDocument'){
 if(!opaque(doc.id)||!Number.isSafeInteger(doc.revision)||doc.revision<1)throw new Error('Invalid document');
 const version=generation;const route=location.hash;const key=requestKey();
 const remove=button(label,()=>{
  if(disposeDialog||version!==generation||location.hash!==route)return;
  const dispose=deletionDialog({root:main,opener:remove,current:()=>version===generation&&location.hash===route,
   labels:{title:t(label==='deletePhoto'?'deletePhotoConfirm':label==='deletePage'?'deletePageConfirm':'deleteDocumentConfirm'),description:t(label==='deletePage'?'deleteLastPageDescription':'deleteDocumentDescription'),cancel:t('cancel'),remove:t(label),retry:t('checkDeletion'),reload:t('reloadDocument'),changed:t('changed'),expired:t('requestExpired'),failed:t('failed'),uncertain:t('deleteUncertain')},
   request:signal=>api('/api/documents/'+doc.id,'DELETE',undefined,key,doc.revision,signal),
   classify:err=>err instanceof APIError&&err.status===410?'expired':err instanceof APIError&&err.status===412?'changed':err instanceof APIError&&err.status>=400&&err.status<500?'failed':'uncertain',
   accepted:value=>{const job=value as any;if(!opaque(job?.id)||job.document_id!==doc.id||job.kind!=='cleanup'||!jobStates.has(job.state))throw new Error('Invalid cleanup job');navigate('#/history')},
   reload:()=>void render(),closed:()=>{if(disposeDialog===dispose)disposeDialog=null}});
  disposeDialog=dispose;
 });remove.className='danger document-delete';return remove;
}

async function history(list:HTMLElement,cursor:string|null,version:number){
 const result=await api('/api/documents?limit=20'+(cursor?'&cursor='+encodeURIComponent(cursor):''));if(version!==generation)return;
 if(!result.items.length&&!cursor)list.append(text('p',t('empty')));
 for(const doc of result.items){
  if(!opaque(doc.id))throw new Error('Invalid document');const row=document.createElement('article');row.className='document-row';row.dataset.documentId=doc.id;
  const thumbnail=document.createElement('div');thumbnail.className='history-thumb';const page=doc.pages?.[0];
  if(page&&opaque(page.id)){if(page.source_type==='pdf')thumbnail.append(pdfPreview(doc.id,page,0,true));else{const link=document.createElement('a');link.href='#/document/'+doc.id;link.setAttribute('aria-label',t('open')+' '+documentName(doc));const image=document.createElement('img');image.alt=t('preview');image.loading='lazy';image.src='/api/documents/'+doc.id+'/pages/'+page.id+'/thumbnail';image.style.transform=`rotate(${page.rotation}deg)`;imageFallback(image,'/api/documents/'+doc.id+'/pages/'+page.id+'/preview');link.append(image);thumbnail.append(link)}}
  const info=document.createElement('div');info.className='history-info';const open=button('open',()=>navigate('#/document/'+doc.id));open.textContent=documentName(doc);open.setAttribute('aria-label',t('open')+' '+documentName(doc));open.dataset.documentOpen='true';open.className='history-title';info.append(open,text('span',`${new Intl.NumberFormat(locale).format(doc.page_count)} ${t('pages')}`));const active=activeJob(doc);if(active){const processing=document.createElement('a');processing.className='history-processing';processing.href='#/job/'+active;processing.setAttribute('aria-label',t('viewJob'));processing.title=t('viewJob');const label=text('span',t('running'));label.id='history-job-'+active;processing.append(label,processingGauge(label.id));info.append(processing)};
  row.append(thumbnail,info,iconButton(documentDeleteButton(doc),'deleteDocument'));list.append(row)
 }
 if(result.next_cursor){const more=button('more',()=>void mutation(more,async()=>{await history(list,result.next_cursor,version);more.remove()}));more.className='more-button';list.append(more)}
}
function processingGauge(labelID:string){const gauge=document.createElement('progress');gauge.className='job-progress';gauge.max=1;gauge.setAttribute('aria-labelledby',labelID);return gauge}
const processingStates=new Set(['queued','running','cancelling','reconciling']);
const jobStates=new Set(['queued','running','cancelling','cancelled','succeeded','failed','interrupted','reconciling']);
async function loadJob(id:string,version:number,attempts=0,inline=false){
 if(version!==generation||document.visibilityState==='hidden')return;
 const controller=new AbortController();jobAbort=controller;
 const schedule=(delay:number,retries=0)=>{if(version===generation&&document.visibilityState==='visible')jobTimer=setTimeout(()=>void loadJob(id,version,retries,inline),delay)};
 try{
  const job=await api('/api/jobs/'+id,'GET',undefined,undefined,undefined,controller.signal);
  if(version!==generation||controller.signal.aborted)return;
  if(!opaque(job.id)||job.id!==id||!opaque(job.document_id)||!jobStates.has(job.state)||!['scan','render','ocr','cleanup','import'].includes(job.kind))throw new Error('Invalid job');
  if(job.kind!=='cleanup'&&job.state==='succeeded'){const download=saveJob===id;saveJob='';if(location.hash==='#/document/'+job.document_id)await render();else{navigate('#/document/'+job.document_id);if(download)await render()}if(download&&location.hash==='#/document/'+job.document_id)location.assign('/api/documents/'+job.document_id+'/download');return}
  const reason:Key|undefined=job.kind==='import'&&job.state==='failed'?new Map<string,Key>([['pdf_invalid','pdfInvalid'],['pdf_encrypted','pdfEncrypted'],['pdf_unsupported','pdfUnsupported'],['pdf_limit','pdfLimit'],['photo_invalid','photoInvalid'],['photo_limit','photoLimit']]).get(job.error_code):undefined;
  if(job.kind==='cleanup'){navigate('#/history');return}
  if(job.state==='failed'&&!inline){let target='';if(job.kind!=='import'){try{const doc=await api('/api/documents/'+job.document_id);if(doc.page_count>0)target='#/document/'+job.document_id}catch(err){if(!(err instanceof APIError&&err.status===404))throw err}}if(version!==generation)return;saveJob='';navigate(target,reason||'failed');return}
  const working=['queued','running'].includes(job.state);const progress:Key=working?(job.kind==='scan'?'readingOriginal':job.kind==='import'?'importingPDF':'preparingFile'):job.state==='failed'?'failedState':job.state as Key;
  if(inline){const line=main.querySelector('#document-progress');if(line&&line.textContent!==t(progress))line.textContent=t(progress);const cancel=main.querySelector<HTMLButtonElement>('[data-job-cancel]');if(cancel){const focused=document.activeElement===cancel;cancel.hidden=!working;if(focused&&!working)focusHeading()}if(processingStates.has(job.state))schedule(750);else{saveJob='';await render();status.textContent=t(progress)}return}
  status.textContent='';const viewKey=`${id}:${progress}:${reason||""}:${locale}`;
  if(jobViewKey===viewKey){if(processingStates.has(job.state))schedule(750);return}
  const focused=main.contains(document.activeElement)?document.activeElement:null;
  const focusedLabel=focused instanceof HTMLButtonElement?focused.textContent:null;
  jobViewKey=viewKey;main.replaceChildren();heading('job');const state=text('p',t(progress));state.id='job-state';state.setAttribute('role','status');main.append(state);if(processingStates.has(job.state))main.append(processingGauge(state.id));
  if(reason)main.append(text('p',t(reason)));
  const actions=document.createElement('div');actions.className='job-actions';main.append(actions);
  actions.append(button('history',()=>navigate('#/history')));
  actions.append(button('open',()=>navigate('#/document/'+job.document_id)));
  if(['queued','running'].includes(job.state)){
   const key=requestKey();const cancel=button('cancel',()=>void mutation(cancel,async()=>{stopJob();try{await api('/api/jobs/'+id+'/cancel','POST',undefined,key);await render();if(location.hash==='#/job/'+id&&document.activeElement===document.body)focusHeading()}catch(err){void loadJob(id,version);throw err}}));actions.append(cancel);
  }
  if(focused&&document.activeElement===document.body){const replacement=Array.from(main.querySelectorAll('button')).find(b=>b.textContent===focusedLabel);if(replacement)replacement.focus();else focusHeading()}
  if(processingStates.has(job.state))schedule(750);
 }catch(err){
  if(version!==generation||controller.signal.aborted)return;
  if(err instanceof APIError&&err.status===404&&!inline){navigate('', 'failed');return}
  if(err instanceof APIError&&[429,503].includes(err.status)&&attempts<3){error(err);schedule(err.retryAfter*1000,attempts+1)}
  else{if(!inline)main.replaceChildren(button('retry',()=>void render()));else{saveJob='';const line=main.querySelector('#document-progress');if(line)line.textContent=t('failed');const gauge=main.querySelector<HTMLProgressElement>('.job-progress');if(gauge)gauge.hidden=true;main.append(button('retry',()=>void render()))}error(err)}
 }
}

function activeJob(doc:any):string|null{if(doc.active_job_id==null)return null;if(!opaque(doc.active_job_id))throw new Error('Invalid active job');return doc.active_job_id}

function pdfPreview(documentID:string,page:any,index:number,thumbnail=false){
 if(!opaque(documentID)||!opaque(page.id)||![0,90,180,270].includes(page.rotation))throw new Error('Invalid PDF preview');
 const url='/api/documents/'+documentID+'/pages/'+page.id+'/preview';const view=generation;
 const preview=createPDFPreview({url,label:`${t('page')} ${index+1}`,rotation:page.rotation,loading:t('loading'),failed:t('failed'),retry:t('retry'),maxWidth:thumbnail?240:1200,current:()=>view===generation});disposePreviews.push(preview.dispose);
 const link=document.createElement('a');if(thumbnail){link.href='#/document/'+documentID;link.setAttribute('aria-label',t('open'));const canvas=preview.element.querySelector('canvas')!;canvas.replaceWith(link);link.append(canvas)}else{link.href=url;link.target='_blank';link.rel='noopener noreferrer';link.textContent=t('openPDFPage');preview.element.append(link)}return preview.element;
}

function showDocument(doc:any){
 const showGeneration=generation;
 if(!opaque(doc.id)||!Number.isSafeInteger(doc.revision)||!Array.isArray(doc.pages))throw new Error('Invalid document');const active=activeJob(doc),photo=doc.purpose==='photo';const header=document.createElement('div');header.className='document-header';const title=document.createElement('div');const name=text('h1',documentName(doc));name.dataset.documentTitle=doc.id;title.append(name);document.title=documentName(doc)+' · Scanner';title.append(text('p',`${t('expires')}: ${new Intl.DateTimeFormat(locale,{dateStyle:'medium',timeStyle:'short'}).format(new Date(doc.expires_at))}`));header.append(title);main.append(header);const toolbar=document.createElement('div');toolbar.className='document-toolbar';main.append(toolbar);const secondary=document.createElement('details');secondary.className='document-more';secondary.append(text('summary',t('documentOptions')));const other=document.createElement('div');other.className='document-more-actions';other.append(documentDeleteButton(doc));secondary.append(other);
 if(active){if(doc.output?.revision!==doc.revision){const save=button(photo?'saveImage':'savePDF',()=>{});save.disabled=true;save.className='primary';toolbar.append(save)}const progress=text('p',t('preparingFile'));progress.id='document-progress';progress.dataset.jobId=active;progress.setAttribute('role','status');toolbar.append(progress,processingGauge(progress.id));const cancel=button('cancel',()=>void mutation(cancel,async()=>{await api('/api/jobs/'+active+'/cancel','POST',undefined,requestKey());await render()}));cancel.dataset.jobCancel='true';toolbar.append(cancel);void loadJob(active,generation,0,true)}
 if(doc.output?.revision===doc.revision){const link=document.createElement('a');link.href='/api/documents/'+doc.id+'/download';link.className='download-link';link.textContent=t(photo?'saveImage':'savePDF');toolbar.append(link)}
 if(!active&&doc.pages.length&&['review','ready','failed'].includes(doc.state)&&doc.output?.revision!==doc.revision){
  const key=requestKey();const create=button(photo?'saveImage':'savePDF',()=>void mutation(create,async()=>{
   const settings=photo?{format:doc.photo_format==='png'?'png':'jpeg',ocr:{enabled:false}}:{format:'pdf',pdf_encoding:'jpeg',ocr:{enabled:false}};
   const job=await api('/api/documents/'+doc.id+'/render','POST',settings,key,doc.revision);if(!opaque(job.id))throw new Error('Invalid job');saveJob=job.id;saveDocument=doc.id;navigate('#/document/'+doc.id)
  }));create.className='primary';toolbar.append(create)
 }
 if(!active&&doc.pages.length&&['review','ready','failed'].includes(doc.state)){const settings=button('outputSettings',()=>outputDialog(doc));other.prepend(settings)}
 if(!active&&['review','ready','failed'].includes(doc.state)&&doc.purpose==='document'&&!doc.pages.some((page:any)=>page.source_type==='photo')){const next=button('scanNext',()=>{next.disabled=true;immediateScan=doc.id;navigate('#/document/'+doc.id+'/scan')});toolbar.append(next)}
 toolbar.append(secondary);
 if(!photo&&!active&&doc.output?.revision===doc.revision&&['review','ready'].includes(doc.state)){const delivery=deliveryControl({doc,name:documentName(doc),t,api,current:()=>showGeneration===generation});main.append(delivery.element);disposePreviews.push(delivery.dispose)}
 if(!photo){const results=ocrResults({doc,locale,t,api,refresh:render,stopJob,current:()=>showGeneration===generation});main.append(results.element);disposePreviews.push(results.dispose)}
 const grid=document.createElement('div');grid.className=photo||doc.pages.some((page:any)=>page.source_type==='photo')?'preview-grid photo-review':'preview-grid';
 const editable=!active&&['review','ready','failed'].includes(doc.state);
 let editing=false;
 const controls:HTMLButtonElement[]=[];
 const edit=async(pageID:string,actionKey:Key,action:()=>Promise<void>)=>{
  if(editing||!editable)return;editing=true;for(const control of controls)control.disabled=true;status.textContent='';const view=generation;const route=location.hash;let expectedView=view;
  try{await action();if(view===generation){expectedView=view+1;await render()}}catch(err){if(view===generation)error(err)}finally{
   editing=false;for(const control of controls)control.disabled=control.dataset.unavailable==='true';
   if(generation===expectedView&&location.hash===route&&document.activeElement===document.body){const card=main.querySelector(`[data-page-id="${pageID}"]`);const target=card?.querySelector<HTMLButtonElement>(`[data-page-action="${actionKey}"]:not(:disabled)`)??card?.querySelector<HTMLButtonElement>('[data-page-action="rotate"]:not(:disabled)');if(target){const menu=target.closest('details');if(menu)menu.open=true;target.focus()}else focusHeading()}
  }
 };
 for(const [index,page] of doc.pages.entries()){
  if(!opaque(page.id)||![0,90,180,270].includes(page.rotation))throw new Error('Invalid page');
  const card=document.createElement('article');card.className='preview-card';card.dataset.pageId=page.id;
  let preview:HTMLElement;
  if(page.source_type==='pdf'){preview=pdfPreview(doc.id,page,index);card.dataset.pageLabel=`PDF ${index+1}`}else{
  const image=document.createElement('img');image.dataset.preview='/api/documents/'+doc.id+'/pages/'+page.id+'/preview';image.dataset.thumbnail='/api/documents/'+doc.id+'/pages/'+page.id+'/thumbnail';image.src=image.dataset.thumbnail;imageFallback(image,image.dataset.preview);image.alt=`${t(photo?'photo':'document')} ${new Intl.NumberFormat(locale).format(index+1)}`;image.loading='lazy';image.style.transform=`rotate(${page.rotation}deg)`;preview=image;
  if(page.source_type==='photo'){
   const frame=document.createElement('div');frame.className='photo-page-preview';frame.append(image);preview=frame;
   image.style.transform=`translate(-50%,-50%) rotate(${page.rotation}deg)`;
   image.addEventListener('load',()=>{const sideways=page.rotation%180!==0;frame.style.setProperty('--preview-ratio',String(sideways?image.naturalHeight/image.naturalWidth:image.naturalWidth/image.naturalHeight));image.style.width=(sideways?100*image.naturalWidth/image.naturalHeight:100)+'%'});
  }
  }
  const actions=document.createElement('div');actions.className='page-actions';const number=text('span',`${t('page')} ${index+1}`);number.className='page-number';if(!photo)actions.append(number);
  const add=(label:Key,action:()=>void,unavailable=false)=>{const control=button(label,action);control.dataset.pageAction=label;control.disabled=!editable||unavailable;control.dataset.unavailable=String(control.disabled);controls.push(control);actions.append(iconButton(control,label));return control};
  const rotateKey=requestKey();add('rotate',()=>void edit(page.id,'rotate',async()=>{await api('/api/documents/'+doc.id+'/pages/'+page.id,'PATCH',{rotation:(page.rotation+90)%360},rotateKey,doc.revision)}));
  if(page.source_type==='photo'&&page.photo){
   const crop=photoCorrectionControl({t,page,current:()=>showGeneration===generation,nextKey:requestKey,changed:render,
    apply:async(body,rotation,key,signal)=>{const job=await api('/api/documents/'+doc.id+'/pages/'+page.id+'/correction/manual','POST',body,key,doc.revision,signal);if(!opaque(job?.id))throw new Error('Invalid crop job');const deadline=Date.now()+120000;for(;;){const value=await api('/api/jobs/'+job.id,'GET',undefined,undefined,undefined,signal);if(value.state==='succeeded')break;if(!['queued','running'].includes(value.state)||Date.now()>deadline)throw new APIError(409);await new Promise<void>((resolve,reject)=>{const done=()=>{signal.removeEventListener('abort',abort);resolve()};const timer=setTimeout(done,500);const abort=()=>{clearTimeout(timer);reject(new DOMException('Aborted','AbortError'))};signal.addEventListener('abort',abort,{once:true})})}if(rotation!==page.rotation){const current=await api('/api/documents/'+doc.id,'GET',undefined,undefined,undefined,signal);if(current.pages.find((item:any)=>item.id===page.id)?.rotation!==rotation){if(current.revision!==job.input_revision+1)throw new APIError(412);await api('/api/documents/'+doc.id+'/pages/'+page.id,'PATCH',{rotation},requestKey(),current.revision,signal)}}},
    undo:async(key,signal)=>{await api('/api/documents/'+doc.id+'/pages/'+page.id+'/correction','DELETE',undefined,key,doc.revision,signal)},classify:err=>err instanceof APIError?{message:t(err.status===409||err.status===412?'changed':'correctionFailed'),unknown:false}:{message:t('correctionUnknown'),unknown:true}});crop.button.disabled=!editable;controls.push(crop.button);actions.append(crop.button);disposePreviews.push(crop.dispose);
  }else if(page.correction&&page.source_type!=='pdf'){
   const correction=correctionControl({documentID:doc.id,pageID:page.id,originalURL:preview.dataset?.preview||page.preview_url,revision:doc.revision,correction:page.correction,current:()=>showGeneration===generation,nextKey:requestKey,
    apply:(suggestion,source,key,signal)=>api('/api/documents/'+doc.id+'/pages/'+page.id+'/correction','POST',{suggestion_id:suggestion,source_sha256:source},key,doc.revision,signal),
    undo:(key,signal)=>api('/api/documents/'+doc.id+'/pages/'+page.id+'/correction','DELETE',undefined,key,doc.revision,signal),
    changed:render,
    classify:err=>err instanceof APIError&&(err.status===409||err.status===412)?{message:t('changed'),unknown:false}:err instanceof APIError?{message:t('correctionFailed'),unknown:false}:{message:t('correctionUnknown'),unknown:true},
    labels:{compare:t('correctionCompare'),title:t('correctionTitle'),original:t('correctionOriginal'),suggested:t('correctionSuggested'),apply:t('correctionApply'),keep:t('correctionKeep'),undo:t('correctionUndo'),reprocess:t('correctionReprocess'),unsafe:t('correctionUnsafe'),transform:t('correctionTransform'),confidence:t('correctionConfidence'),unavailable:t('correctionUnavailable'),previewUnavailable:t('correctionPreviewUnavailable')}});
   correction.button.disabled=!editable;correction.button.dataset.unavailable=String(correction.button.disabled);controls.push(correction.button);actions.append(iconButton(correction.button,'correctionCompare'));disposePreviews.push(correction.dispose);
  }
  if(!photo)for(const delta of [-1,1]){const order=doc.pages.map((item:any)=>item.id);const target=index+delta;if(target>=0&&target<order.length)[order[index],order[target]]=[order[target],order[index]];const key=requestKey();add(delta<0?'moveEarlier':'moveLater',()=>void edit(page.id,delta<0?'moveEarlier':'moveLater',async()=>{await api('/api/documents/'+doc.id+'/pages/order','PUT',{page_ids:order},key,doc.revision)}),target<0||target>=order.length)}
  if(doc.pages.length===1){const remove=documentDeleteButton(doc,photo?'deletePhoto':'deletePage');remove.disabled=!editable;remove.dataset.unavailable=String(remove.disabled);controls.push(remove);actions.append(iconButton(remove,'deletePage'))}else{
  const deleteKey=requestKey();add('deletePage',()=>{
   if(disposeDialog||editing)return;const dialog=document.createElement('dialog');dialog.setAttribute('aria-labelledby','delete-page-title');const title=text('h2',t('deletePageConfirm'));title.id='delete-page-title';
   const cancel=button('cancel',()=>dialog.close());const remove=button('deletePage',()=>{dialog.close();void edit(page.id,'deletePage',async()=>{await api('/api/documents/'+doc.id+'/pages/'+page.id,'DELETE',undefined,deleteKey,doc.revision)})});
   dialog.append(title,cancel,remove);main.append(dialog);const dispose=()=>{dialog.remove();if(disposeDialog===dispose)disposeDialog=null};disposeDialog=dispose;dialog.addEventListener('close',dispose,{once:true});dialog.addEventListener('click',event=>{const box=dialog.getBoundingClientRect();if(event.target===dialog&&(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom))dialog.close()});dialog.showModal();cancel.focus()
  })};card.append(preview,actions);grid.append(card)
 }main.append(grid)
}
function outputDialog(doc:any){
 const view=generation;
 const dialog=document.createElement('dialog');dialog.id='output-settings';dialog.setAttribute('aria-labelledby','output-title');
 if(disposeDialog)return;
 const title=text('h2',t('outputSettings'));title.id='output-title';
 const label=document.createElement('label');label.htmlFor='output-choice';label.textContent=t(doc.purpose==='photo'?'format':'encoding');
 const select=document.createElement('select');select.id='output-choice';
 for(const value of doc.purpose==='photo'?['jpeg','png']:['jpeg','lossless']){const option=document.createElement('option');option.value=value;option.textContent=t(value as Key);select.append(option)}
 if(doc.purpose==='photo')select.value=doc.photo_format==='png'?'png':'jpeg';
 const hint=text('p','');hint.setAttribute('role','alert');
 let pending=false,fingerprint='',key='';
 let requestController:AbortController|null=null;
 const ocr:ReturnType<typeof ocrControls>|null=doc.purpose==='photo'?null:ocrControls(signal=>api('/api/capabilities','GET',undefined,undefined,undefined,signal),{enable:t('searchablePDF'),model:t('ocrModel'),languages:t('ocrLanguages'),quality:t('ocrQuality'),chooseModel:t('chooseModel'),chooseQuality:t('chooseQuality'),loading:t('loading'),unavailable:t('ocrUnavailable'),retry:t('retry'),external:t('ocrExternal'),presets:{auto:t('automatic'),standard:t('standardQuality'),fast:t('fastQuality'),accurate:t('accurateQuality')}},locale,()=>{submit.disabled=pending||!ocr!.valid();dialog.classList.toggle('with-ocr',ocr!.enabled())});
 const close=button('close',()=>{if(!pending)dialog.close()});
 const submit=button(doc.purpose==='photo'?'saveImage':'savePDF',async()=>{
  if(pending||ocr&&!ocr.valid())return;
  const command=doc.purpose==='photo'?{format:select.value,ocr:{enabled:false}}:{format:'pdf',pdf_encoding:select.value,ocr:ocr!.command()};
  const digest=JSON.stringify(command);if(digest!==fingerprint){fingerprint=digest;key=requestKey()}
  pending=true;submit.disabled=true;select.disabled=true;close.disabled=true;ocr?.lock(true);hint.textContent='';
  const controller=new AbortController();requestController=controller;const timeout=setTimeout(()=>controller.abort(),15000);
  try{const job=await api('/api/documents/'+doc.id+'/render','POST',command,key,doc.revision,controller.signal);if(!opaque(job.id))throw new Error('Invalid job');if(view!==generation||!dialog.isConnected)return;saveJob=job.id;saveDocument=doc.id;dialog.close();navigate('#/document/'+doc.id)}
  catch(err){if(view===generation&&dialog.isConnected)hint.textContent=t(err instanceof APIError&&err.status===412?'changed':err instanceof APIError&&err.status===422?'unsupported':'failed')}
  finally{clearTimeout(timeout);pending=false;submit.disabled=ocr?!ocr.valid():false;select.disabled=false;close.disabled=false;ocr?.lock(false)}
 });submit.className='primary';
 const actions=document.createElement('div');actions.className='output-actions';actions.append(close,submit);const body=document.createElement('div');body.className='output-body';body.append(label,select);if(ocr)body.append(ocr.element);body.append(hint);dialog.append(title,body,actions);main.append(dialog);
 const dispose=()=>{ocr?.dispose();requestController?.abort();dialog.remove();if(disposeDialog===dispose)disposeDialog=null};disposeDialog=dispose;
 dialog.addEventListener('cancel',event=>{if(pending)event.preventDefault()});
 dialog.addEventListener('click',event=>{const box=dialog.getBoundingClientRect();if(!pending&&event.target===dialog&&(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom))dialog.close()});
 dialog.addEventListener('close',dispose,{once:true});dialog.showModal();ocr?.start();
}

document.querySelector('#home')!.addEventListener('click',()=>navigate(''));
document.querySelector('#history')!.addEventListener('click',()=>navigate('#/history'));
document.querySelector<HTMLSelectElement>('#locale')!.addEventListener('change',e=>{locale=(e.target as HTMLSelectElement).value==='ja'?'ja':'en';try{localStorage.setItem('scanner.locale',locale)}catch{}localize();void render()});
document.querySelector('#logout')!.addEventListener('click',()=>{const b=document.querySelector<HTMLButtonElement>('#logout')!;void mutation(b,async()=>{await api(session?.principal_kind==='guest'?'/auth/guest/end':'/auth/logout','POST',undefined,requestKey());navigate('');await render()})});
window.addEventListener('hashchange',()=>void render());void render();

window.addEventListener('pageshow',event=>{if(event.persisted)void render()});

document.addEventListener('visibilitychange',()=>{if(document.hidden)stopJob();else if(location.hash.startsWith('#/job/'))void render();else{const job=main.querySelector<HTMLElement>('#document-progress')?.dataset.jobId;if(job&&opaque(job))void loadJob(job,generation,0,true)}});

document.addEventListener('click',event=>{if(!(event.target instanceof Node))return;for(const menu of document.querySelectorAll<HTMLDetailsElement>('details[open]')){if(!menu.contains(event.target)||event.target instanceof Element&&event.target.closest('.document-more-actions button'))menu.open=false}});
document.addEventListener('keydown',event=>{if(event.key!=='Escape')return;const menu=(event.target instanceof Element?event.target.closest<HTMLDetailsElement>('details[open]'):null)||document.querySelector<HTMLDetailsElement>('.document-more[open]');if(menu){event.preventDefault();menu.open=false;menu.querySelector('summary')?.focus()}});
