import {requestKey,observeRequestTime} from './request-key';
import {deletionDialog} from './deletion';
import {reviewViews,type ReviewState} from './review';
import {ocrControls} from './ocr';
import {passwordLogin} from './login';
import {captureControls} from './capture';
import {importControls} from './import';
const messages = {
 en:{signinHelp:'Sign in to scan, edit and save your documents.',guestHelp:'Start a session to scan, edit and save your documents.',pdfInvalid:'This PDF is damaged or could not be read.',pdfEncrypted:'Password-protected PDFs cannot be imported. Save an unencrypted copy first.',pdfUnsupported:'This PDF contains features that cannot be imported.',pdfLimit:'This PDF exceeds the page or processing limits.',importPDF:'Import PDF',pdfFile:'PDF file',importHelp:'Import an existing PDF, then insert scans, reorder or replace pages. Files are stored temporarily with your documents.',importLimit:'Maximum {size} MiB. Encrypted and unsupported PDFs cannot be imported.',importInvalid:'Choose a non-empty PDF within the size limit.',importPreparing:'Preparing PDF…',importSending:'Uploading PDF…',importUncertain:'The import may have started. Check the same request or open History before importing again.',checkImport:'Check import request',importRejected:'The PDF could not be imported. Check the file and available storage.',openPDFPage:'Open PDF page',pdfPageNotice:'Original PDF preview. Export rotation: {rotation}°.',insertScan:'Insert scan before',replaceScan:'Replace with scan',replacementNotice:'The original page is kept until the new scan succeeds.',skipContent:'Skip to content',rateLimited:"Too many requests. Wait {seconds}s, then try again.",requestExpired:"This request expired. Reload this view before choosing another action.",reloadView:"Reload view",deleteDocument:'Delete document',deleteDocumentConfirm:'Delete this document?',deleteDocumentDescription:'This removes the document and its pages from this service and requests cancellation of any work in progress. Copies already downloaded stay on your device.',deleteUncertain:'Deletion may have started. Check the request again before leaving this screen.',checkDeletion:'Check deletion request',reloadDocument:'Reload document',deletionStatus:'Deletion status',deletionPending:'This document is no longer available. File removal is pending.',deletionDone:'Deleted from this service. Copies already downloaded stay on your device.',deleted:'Deleted',overview:'Overview',pageView:'Page view',previousPage:'Previous page',nextPage:'Next page',page:'Page',moveEarlier:'Move earlier',moveLater:'Move later',deletePage:'Delete page',deletePageConfirm:'Delete this page?',viewJob:'View processing status',outputFormat:'Output format',chooseScanner:'Select scanner',changeScanner:'Change scanner',searchScanners:'Search scanners',noScanners:'No scanners configured',noMatches:'No matching scanners',scannerIdle:'Available',scannerBusy:'In use',scannerUnknown:'Not ready',scanSettings:'Scan settings',size:'Paper size',color:'Color',gray:'Grayscale',dpi:'DPI',advanced:'Advanced',apply:'Apply',scan:'Scan',scanNext:'Scan next page',settingsChanged:'Settings changed. Review the scan settings.',scanRejected:'Scanning could not start. Check the scanner and try again.',scanUncertain:'The scan may have started. Check History before scanning again.',a4:'A4',a5:'A5',a6:'A6',letter:'Letter',photo10x15:'10 × 15 cm',credentialsTooLong:'The username or password is too long.',username:'Username',password:'Password',invalidCredentials:'Incorrect username or password.',loginExpired:'Reset this session to sign in again.',loginLimited:'Too many sign-in attempts. Try again shortly.',automatic:'Automatic',standardQuality:'Standard',fastQuality:'Fast',accurateQuality:'Accurate',ocrModel:'OCR model',ocrLanguages:'OCR languages',ocrQuality:'Quality',chooseModel:'Select a model',chooseQuality:'Select quality',ocrUnavailable:'OCR is unavailable',ocrExternal:'Images will be sent to {destination} for OCR.',unsupported:'These settings cannot process this document. Check the OCR model and languages.',outputSettings:'Output settings',encoding:'PDF image encoding',format:'Image format',jpeg:'JPEG',png:'PNG',lossless:'Lossless',close:'Close',createPDF:'Create PDF',saveImage:'Save image',job:'Processing status',cancel:'Cancel',navigation:'Main navigation',appearance:'Appearance',retry:'Try again',home:'Home',history:'History',logout:'Sign out',end:'End session',language:'Language',loading:'Loading…',start:'Start session',signin:'Sign in',reset:'Reset session',empty:'No documents',open:'Open',document:'Document',photo:'Photo',back:'Back',more:'Show more',download:'Download',rotate:'Rotate',failed:'The request could not be completed. Try again.',changed:'This document changed. Reload it before editing.',scanner:'Scanner',unavailable:'Scanning unavailable',type:'Original type',pages:'Pages',expires:'Expires',light:'Light',dark:'Dark',system:'System',session:'Session',queued:'Queued',running:'Processing',cancelling:'Cancelling',cancelled:'Cancelled',succeeded:'Completed',review:'Review',processing:'Processing',ready:'Ready',scanning:'Scanning',interrupted:'Interrupted',reconciling:'Checking status',deleting:'Deleting',expired:'Expired',failedState:'Failed'},
 ja:{signinHelp:'ログインして、スキャンやPDFの編集・保存を始めましょう。',guestHelp:'利用を開始して、スキャンやPDFの編集・保存を行えます。',pdfInvalid:'PDFが破損しているか、読み取れませんでした。',pdfEncrypted:'パスワードで保護されたPDFは取り込めません。暗号化されていないコピーを用意してください。',pdfUnsupported:'このPDFには取り込みに未対応の機能が含まれています。',pdfLimit:'PDFのページ数または処理容量が上限を超えています。',importPDF:'PDFを取り込む',pdfFile:'PDFファイル',importHelp:'既存PDFを取り込み、スキャンの挿入・ページの並べ替え・置換ができます。ファイルは書類と同じく一時保存されます。',importLimit:'上限 {size} MiB。暗号化されたPDFや未対応のPDFは取り込めません。',importInvalid:'上限以内の空でないPDFを選択してください。',importPreparing:'PDFを準備中…',importSending:'PDFをアップロード中…',importUncertain:'取り込みが開始された可能性があります。再操作の前に同じ受付状況か履歴を確認してください。',checkImport:'取り込みの受付状況を確認',importRejected:'PDFを取り込めませんでした。ファイルと空き容量を確認してください。',openPDFPage:'PDFページを開く',pdfPageNotice:'元PDFのプレビューです。出力時の回転：{rotation}°。',insertScan:'前にスキャンを挿入',replaceScan:'スキャンで置換',replacementNotice:'新しいスキャンが成功するまで元のページを保持します。',skipContent:'本文へ移動',rateLimited:"操作が集中しています。{seconds}秒ほど待ってから、もう一度お試しください。",requestExpired:"この操作の有効期限が切れました。画面を読み込み直してから操作を選んでください。",reloadView:"画面を再読み込み",deleteDocument:'書類を削除',deleteDocumentConfirm:'この書類を削除しますか？',deleteDocumentDescription:'このサービスから書類と全ページを削除し、進行中の処理には中止を要求します。端末に保存したコピーは残ります。',deleteUncertain:'削除が受け付けられた可能性があります。この画面を離れる前に、もう一度受付状況を確認してください。',checkDeletion:'削除の受付状況を確認',reloadDocument:'書類を再読み込み',deletionStatus:'削除の状況',deletionPending:'書類は利用できなくなりました。ファイルの削除完了を待っています。',deletionDone:'このサービスから削除しました。端末に保存したコピーは残ります。',deleted:'削除済み',overview:'一覧',pageView:'ページ表示',previousPage:'前のページ',nextPage:'次のページ',page:'ページ',moveEarlier:'前へ移動',moveLater:'後ろへ移動',deletePage:'ページを削除',deletePageConfirm:'このページを削除しますか？',viewJob:'処理状況を開く',outputFormat:'保存形式',chooseScanner:'スキャナーを選択',changeScanner:'スキャナーを変更',searchScanners:'スキャナーを検索',noScanners:'スキャナーが登録されていません',noMatches:'一致するスキャナーはありません',scannerIdle:'利用可能',scannerBusy:'使用中',scannerUnknown:'準備未完了',scanSettings:'スキャン設定',size:'原稿サイズ',color:'カラー',gray:'グレースケール',dpi:'DPI',advanced:'詳細設定',apply:'適用',scan:'読み取る',scanNext:'次の原稿を読み取る',settingsChanged:'設定が変更されています。スキャン設定を確認してください。',scanRejected:'読み取りを開始できませんでした。スキャナーを確認してください。',scanUncertain:'読み取りが開始された可能性があります。再操作の前に履歴を確認してください。',a4:'A4',a5:'A5',a6:'A6',letter:'レター',photo10x15:'10 × 15 cm',credentialsTooLong:'ユーザー名またはパスワードが長すぎます。',username:'ユーザー名',password:'パスワード',invalidCredentials:'ユーザー名またはパスワードが違います。',loginExpired:'セッションをリセットしてログインし直してください。',loginLimited:'ログインの試行回数が上限に達しました。しばらくしてからお試しください。',automatic:'自動',standardQuality:'標準',fastQuality:'高速',accurateQuality:'高精度',ocrModel:'OCRモデル',ocrLanguages:'OCR言語',ocrQuality:'品質',chooseModel:'モデルを選択',chooseQuality:'品質を選択',ocrUnavailable:'OCRを利用できません',ocrExternal:'OCRのために画像を{destination}へ送信します。',unsupported:'この設定では処理できません。OCRモデルと言語を確認してください。',outputSettings:'出力設定',encoding:'PDFの画像形式',format:'画像形式',jpeg:'JPEG',png:'PNG',lossless:'可逆圧縮',close:'閉じる',createPDF:'PDFを作成',saveImage:'画像を保存',job:'処理状況',cancel:'中止',navigation:'メインメニュー',appearance:'外観',retry:'再読み込み',home:'ホーム',history:'履歴',logout:'ログアウト',end:'利用を終了',language:'表示言語',loading:'読み込み中…',start:'利用を開始',signin:'ログイン',reset:'セッションをリセット',empty:'書類はありません',open:'開く',document:'書類',photo:'写真',back:'戻る',more:'さらに表示',download:'保存',rotate:'回転',failed:'操作を完了できませんでした。再度お試しください。',changed:'書類が更新されています。読み込み直してから編集してください。',scanner:'スキャナー',unavailable:'スキャンを利用できません',type:'原稿の種類',pages:'ページ数',expires:'保存期限',light:'ライト',dark:'ダーク',system:'端末に合わせる',session:'セッション',queued:'待機中',running:'処理中',cancelling:'中止中',cancelled:'中止済み',succeeded:'完了',review:'確認',processing:'処理中',ready:'保存可能',scanning:'スキャン中',interrupted:'中断',reconciling:'状態を確認中',deleting:'削除中',expired:'保存期限切れ',failedState:'失敗'}
} as const;
type Key=keyof typeof messages.en;
let locale:'en'|'ja'='en';try{if(localStorage.getItem('scanner.locale')==='ja')locale='ja'}catch{}
const t=(key:Key)=>messages[locale][key];
const main=document.querySelector<HTMLElement>('#main')!;
const status=document.querySelector<HTMLElement>('#status')!;
const skipLink=document.querySelector<HTMLAnchorElement>('#skip-link')!;
skipLink.addEventListener('click',event=>{event.preventDefault();main.focus()});
const button=(label:Key,action:()=>void)=>{const b=document.createElement('button');b.type='button';b.textContent=t(label);b.addEventListener('click',action);return b};
const text=(tag:string,value:string)=>{const e=document.createElement(tag);e.textContent=value;return e};
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
let jobViewKey='';
let reviewState:ReviewState&{documentID:string}={documentID:'',single:false,pageID:null,index:0};
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
function navigate(hash:string){if(location.hash===hash)void render();else location.hash=hash}
let boot:Promise<any>|null=null;
let renderedHash:string|null=null;
async function bootstrap(){if(!boot)boot=api('/api/session').finally(()=>boot=null);return boot}
async function render(){
 const hash=location.hash;const moveFocus=renderedHash!==null&&renderedHash!==hash;const focused=document.activeElement;
 disposeImport?.();disposeImport=null;disposeCapture?.();disposeCapture=null;disposeDialog?.();disposeDialog=null;disposeLogin?.();disposeLogin=null;stopJob();jobViewKey='';const version=++generation;status.textContent='';main.replaceChildren(text('p',t('loading')));main.setAttribute('aria-busy','true');
 try{
  const current=await bootstrap();if(version!==generation)return;if(!['guest','oidc','simple'].includes(current?.authenticate_type))throw new Error('Invalid authentication type');session=current;document.querySelector<HTMLElement>('#history')!.hidden=!session?.authenticated;document.querySelector<HTMLElement>('#logout')!.hidden=!session?.authenticated;localize();main.replaceChildren();
  if(!session?.authenticated)reviewState={documentID:'',single:false,pageID:null,index:0};
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
  if(route==='/import'){heading('importPDF');main.append(button('back',()=>navigate('')));const controls=importControls({t,current:()=>version===generation,load:signal=>api('/api/capabilities','GET',undefined,undefined,undefined,signal),send:(file,key,sha,signal)=>responseDataFromUpload(file,key,sha,signal),accepted:job=>navigate('#/job/'+job.id),history:()=>navigate('#/history'),reload:()=>void render(),classify:err=>err instanceof APIError&&err.status===410?'expired':err instanceof APIError&&err.status>=400&&err.status<500&&![401,404].includes(err.status)?'rejected':'uncertain'});main.append(controls.element);disposeImport=controls.dispose;void controls.start();return}
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
   heading(mode==='before'?'insertScan':mode==='replace'?'replaceScan':'scanNext');
   main.append(button('back',()=>navigate('#/document/'+id)));
   if(mode==='replace')main.append(text('p',t('replacementNotice')));
   mountCapture(version,doc,placement);return
  }
  if(route.startsWith('/document/')){const id=route.slice('/document/'.length);if(!opaque(id))throw new Error('Invalid document');const value=await api('/api/documents/'+id);if(version!==generation)return;showDocument(value);return}
  if(route==='/history'){heading('history');const list=document.createElement('div');list.className='document-list';main.append(list);await history(list,null,version);return}
  const homeHeading=document.createElement('div');homeHeading.className='home-heading';heading('home',homeHeading);homeHeading.append(button('importPDF',()=>navigate('#/import')));main.append(homeHeading);mountCapture(version);
 }catch(err){if(version===generation){main.replaceChildren(button('retry',()=>void render()));error(err)}}finally{
  if(version===generation){main.setAttribute('aria-busy','false');if(location.hash===hash){renderedHash=hash;if(moveFocus&&(document.activeElement===document.body||document.activeElement===focused&&focused instanceof Element&&focused.closest('nav,.brand')))focusHeading()}}
 }
}
function mountCapture(view:number,doc?:any,placement?:{insert_before_page_id?:string;replace_page_id?:string}){const controls=captureControls({t,current:()=>view===generation,read:signal=>api('/api/scanners','GET',undefined,undefined,undefined,signal),submitLabel:placement?.replace_page_id?'replaceScan':placement?.insert_before_page_id?'insertScan':undefined,submit:(body,key,signal)=>api(doc?'/api/documents/'+doc.id+'/scans':'/api/documents','POST',placement?{...(body as object),...placement}:body,key,doc?.revision,signal),accepted:job=>navigate('#/job/'+job.id),history:()=>navigate('#/history'),expired:err=>err instanceof APIError&&err.status===410,reload:()=>void render(),rejected:err=>err instanceof APIError&&(err.status>=400&&err.status<500||err.status===503&&err.code==='scanner_unavailable'),purpose:doc?.purpose,photoFormat:doc?.photo_format==='png'?'png':'jpeg'});main.append(controls.element);disposeCapture=controls.dispose;controls.start()}

async function responseDataFromUpload(file:File,key:string,sha:string,signal:AbortSignal){return responseData(await fetch('/api/documents/import',{method:'POST',headers:{'Content-Type':'application/pdf','Idempotency-Key':key,'X-Content-SHA256':sha},body:file,credentials:'same-origin',cache:'no-store',redirect:'error',signal}))}

function documentDeleteButton(doc:any){
 if(!opaque(doc.id)||!Number.isSafeInteger(doc.revision)||doc.revision<1)throw new Error('Invalid document');
 const version=generation;const route=location.hash;const key=requestKey();
 const remove=button('deleteDocument',()=>{
  if(disposeDialog||version!==generation||location.hash!==route)return;
  const dispose=deletionDialog({root:main,opener:remove,current:()=>version===generation&&location.hash===route,
   labels:{title:t('deleteDocumentConfirm'),description:t('deleteDocumentDescription'),cancel:t('cancel'),remove:t('deleteDocument'),retry:t('checkDeletion'),reload:t('reloadDocument'),changed:t('changed'),expired:t('requestExpired'),failed:t('failed'),uncertain:t('deleteUncertain')},
   request:signal=>api('/api/documents/'+doc.id,'DELETE',undefined,key,doc.revision,signal),
   classify:err=>err instanceof APIError&&err.status===410?'expired':err instanceof APIError&&err.status===412?'changed':err instanceof APIError&&err.status>=400&&err.status<500?'failed':'uncertain',
   accepted:value=>{const job=value as any;if(!opaque(job?.id)||job.document_id!==doc.id||job.kind!=='cleanup'||!jobStates.has(job.state))throw new Error('Invalid cleanup job');main.replaceChildren();navigate('#/job/'+job.id)},
   reload:()=>void render(),closed:()=>{if(disposeDialog===dispose)disposeDialog=null}});
  disposeDialog=dispose;
 });remove.className='danger document-delete';return remove;
}

async function history(list:HTMLElement,cursor:string|null,version:number){
 const result=await api('/api/documents?limit=20'+(cursor?'&cursor='+encodeURIComponent(cursor):''));if(version!==generation)return;
 if(!result.items.length&&!cursor)list.append(text('p',t('empty')));
 for(const doc of result.items){if(!opaque(doc.id))throw new Error('Invalid document');const row=document.createElement('article');row.className='document-row';const state:Key=doc.state==='failed'?'failedState':doc.state in messages.en?doc.state:'review';const active=activeJob(doc);row.append(text('strong',t(doc.purpose==='photo'?'photo':'document')),text('span',`${t('pages')}: ${new Intl.NumberFormat(locale).format(doc.page_count)} · ${t(state)}`),button('open',()=>navigate('#/document/'+doc.id)));if(active)row.append(button('viewJob',()=>navigate('#/job/'+active)));row.append(documentDeleteButton(doc));list.append(row)}
 if(result.next_cursor){const more=button('more',()=>void mutation(more,async()=>{await history(list,result.next_cursor,version);more.remove()}));list.append(more)}
}
const jobStates=new Set(['queued','running','cancelling','cancelled','succeeded','failed','interrupted','reconciling']);
async function loadJob(id:string,version:number,attempts=0){
 if(version!==generation||document.visibilityState==='hidden')return;
 const controller=new AbortController();jobAbort=controller;
 const schedule=(delay:number,retries=0)=>{if(version===generation&&document.visibilityState==='visible')jobTimer=setTimeout(()=>void loadJob(id,version,retries),delay)};
 try{
  const job=await api('/api/jobs/'+id,'GET',undefined,undefined,undefined,controller.signal);
  if(version!==generation||controller.signal.aborted)return;
  if(!opaque(job.id)||job.id!==id||!opaque(job.document_id)||!jobStates.has(job.state)||!['scan','render','ocr','cleanup','import'].includes(job.kind))throw new Error('Invalid job');
  const reason:Key|undefined=job.kind==='import'&&job.state==='failed'?new Map<string,Key>([['pdf_invalid','pdfInvalid'],['pdf_encrypted','pdfEncrypted'],['pdf_unsupported','pdfUnsupported'],['pdf_limit','pdfLimit']]).get(job.error_code):undefined;
  status.textContent='';const viewKey=`${id}:${job.state}:${reason||""}:${locale}`;
  if(jobViewKey===viewKey){if(['queued','running','cancelling','reconciling'].includes(job.state))schedule(2000);return}
  const focused=main.contains(document.activeElement)?document.activeElement:null;
  const focusedLabel=focused instanceof HTMLButtonElement?focused.textContent:null;
  jobViewKey=viewKey;main.replaceChildren();heading(job.kind==='cleanup'?'deletionStatus':'job');const state=text('p',t(job.kind==='cleanup'&&job.state==='succeeded'?'deleted':job.state==='failed'?'failedState':job.state as Key));state.id='job-state';state.setAttribute('role','status');main.append(state);if(job.kind==='cleanup')main.append(text('p',t(job.state==='succeeded'?'deletionDone':'deletionPending')));
  if(reason)main.append(text('p',t(reason)));
  main.append(button('history',()=>navigate('#/history')));
  if(job.kind!=='cleanup')main.append(button('open',()=>navigate('#/document/'+job.document_id)));
  if(job.kind!=='cleanup'&&['queued','running'].includes(job.state)){
   const key=requestKey();const cancel=button('cancel',()=>void mutation(cancel,async()=>{stopJob();try{await api('/api/jobs/'+id+'/cancel','POST',undefined,key);await render();if(location.hash==='#/job/'+id&&document.activeElement===document.body)focusHeading()}catch(err){void loadJob(id,version);throw err}}));main.append(cancel);
  }
  if(focused&&document.activeElement===document.body){const replacement=Array.from(main.querySelectorAll('button')).find(b=>b.textContent===focusedLabel);if(replacement)replacement.focus();else focusHeading()}
  if(['queued','running','cancelling','reconciling'].includes(job.state))schedule(2000);
 }catch(err){
  if(version!==generation||controller.signal.aborted)return;
  if(err instanceof APIError&&[429,503].includes(err.status)&&attempts<3){error(err);schedule(err.retryAfter*1000,attempts+1)}
  else{main.replaceChildren(button('retry',()=>void render()));error(err)}
 }
}

function activeJob(doc:any):string|null{if(doc.active_job_id==null)return null;if(!opaque(doc.active_job_id))throw new Error('Invalid active job');return doc.active_job_id}

function showDocument(doc:any){
 if(!opaque(doc.id)||!Number.isSafeInteger(doc.revision)||!Array.isArray(doc.pages))throw new Error('Invalid document');const active=activeJob(doc);if(reviewState.documentID!==doc.id)reviewState={documentID:doc.id,single:false,pageID:null,index:0};heading(doc.purpose==='photo'?'photo':'document');main.append(button('back',()=>navigate('#/history')),text('p',`${t('expires')}: ${new Intl.DateTimeFormat(locale,{dateStyle:'medium',timeStyle:'short'}).format(new Date(doc.expires_at))}`));
 if(active){const view=button('viewJob',()=>navigate('#/job/'+active));view.className='primary';main.append(view)}
 if(doc.output?.revision===doc.revision){const link=document.createElement('a');link.href='/api/documents/'+doc.id+'/download';link.className='download-link';link.textContent=t('download');main.append(link)}
 if(!active&&doc.pages.length&&['review','ready','failed'].includes(doc.state)&&doc.output?.revision!==doc.revision){
  const key=requestKey();const create=button(doc.purpose==='photo'?'saveImage':'createPDF',()=>void mutation(create,async()=>{
   const settings=doc.purpose==='photo'?{format:doc.photo_format==='png'?'png':'jpeg',ocr:{enabled:false}}:{format:'pdf',pdf_encoding:'jpeg',ocr:{enabled:false}};
   const job=await api('/api/documents/'+doc.id+'/render','POST',settings,key,doc.revision);if(!opaque(job.id))throw new Error('Invalid job');navigate('#/job/'+job.id)
  }));create.className='primary';main.append(create)
 }
 if(!active&&doc.pages.length&&['review','ready','failed'].includes(doc.state)){const settings=button('outputSettings',()=>outputDialog(doc));main.append(settings)}
 if(!active&&['review','ready','failed'].includes(doc.state)&&(doc.purpose==='document'||!doc.pages.length))main.append(button('scanNext',()=>navigate('#/document/'+doc.id+'/scan')));
 const grid=document.createElement('div');grid.className='preview-grid';
 const editable=!active&&['review','ready','failed'].includes(doc.state);
 let editing=false;
 const controls:HTMLButtonElement[]=[];
 const edit=async(pageID:string,actionKey:Key,action:()=>Promise<void>)=>{
  if(editing||!editable)return;editing=true;for(const control of controls)control.disabled=true;status.textContent='';const view=generation;const route=location.hash;let expectedView=view;
  try{await action();if(view===generation){expectedView=view+1;await render()}}catch(err){if(view===generation)error(err)}finally{
   editing=false;for(const control of controls)control.disabled=control.dataset.unavailable==='true';
   if(generation===expectedView&&location.hash===route&&document.activeElement===document.body){const card=main.querySelector(`[data-page-id="${pageID}"]`);const target=card?.querySelector<HTMLButtonElement>(`[data-page-action="${actionKey}"]:not(:disabled)`)??card?.querySelector<HTMLButtonElement>('[data-page-action="rotate"]:not(:disabled)');if(target)target.focus();else focusHeading()}
  }
 };
 for(const [index,page] of doc.pages.entries()){
  if(!opaque(page.id)||![0,90,180,270].includes(page.rotation))throw new Error('Invalid page');
  const card=document.createElement('article');card.className='preview-card';card.dataset.pageId=page.id;
  let preview:HTMLElement;
  if(page.source_type==='pdf'){const panel=document.createElement('div');panel.className='pdf-page';const link=document.createElement('a');link.href='/api/documents/'+doc.id+'/pages/'+page.id+'/preview';link.target='_blank';link.rel='noopener noreferrer';link.textContent=t('openPDFPage');panel.append(text('strong','PDF'),link,text('p',t('pdfPageNotice').replace('{rotation}',String(page.rotation))));preview=panel;card.dataset.pageLabel=`PDF ${index+1}`}else{
  const image=document.createElement('img');image.src='/api/documents/'+doc.id+'/pages/'+page.id+'/preview';image.dataset.preview=image.src;image.dataset.thumbnail='/api/documents/'+doc.id+'/pages/'+page.id+'/thumbnail';image.alt=`${t(doc.purpose==='photo'?'photo':'document')} ${new Intl.NumberFormat(locale).format(index+1)}`;image.loading='lazy';image.style.transform=`rotate(${page.rotation}deg)`;preview=image;}
  const actions=document.createElement('div');actions.className='page-actions';
  const add=(label:Key,action:()=>void,unavailable=false)=>{const control=button(label,action);control.dataset.pageAction=label;control.disabled=!editable||unavailable;control.dataset.unavailable=String(control.disabled);controls.push(control);actions.append(control);return control};
  if(doc.purpose==='document')add('insertScan',()=>navigate('#/document/'+doc.id+'/scan?before='+page.id));
  add('replaceScan',()=>navigate('#/document/'+doc.id+'/scan?replace='+page.id));
  const rotateKey=requestKey();add('rotate',()=>void edit(page.id,'rotate',async()=>{await api('/api/documents/'+doc.id+'/pages/'+page.id,'PATCH',{rotation:(page.rotation+90)%360},rotateKey,doc.revision)}));
  for(const delta of [-1,1]){const order=doc.pages.map((item:any)=>item.id);const target=index+delta;if(target>=0&&target<order.length)[order[index],order[target]]=[order[target],order[index]];const key=requestKey();add(delta<0?'moveEarlier':'moveLater',()=>void edit(page.id,delta<0?'moveEarlier':'moveLater',async()=>{await api('/api/documents/'+doc.id+'/pages/order','PUT',{page_ids:order},key,doc.revision)}),target<0||target>=order.length)}
  const deleteKey=requestKey();add('deletePage',()=>{
   if(disposeDialog||editing)return;const dialog=document.createElement('dialog');dialog.setAttribute('aria-labelledby','delete-page-title');const title=text('h2',t('deletePageConfirm'));title.id='delete-page-title';
   const cancel=button('cancel',()=>dialog.close());const remove=button('deletePage',()=>{dialog.close();void edit(page.id,'deletePage',async()=>{await api('/api/documents/'+doc.id+'/pages/'+page.id,'DELETE',undefined,deleteKey,doc.revision)})});
   dialog.append(title,cancel,remove);main.append(dialog);const dispose=()=>{dialog.remove();if(disposeDialog===dispose)disposeDialog=null};disposeDialog=dispose;dialog.addEventListener('close',dispose,{once:true});dialog.addEventListener('click',event=>{const box=dialog.getBoundingClientRect();if(event.target===dialog&&(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom))dialog.close()});dialog.showModal();cancel.focus()
  });card.append(preview,actions);grid.append(card)
 }main.append(reviewViews(grid,{overview:t('overview'),pageView:t('pageView'),previous:t('previousPage'),next:t('nextPage'),page:t('page')},reviewState));const deleteArea=document.createElement('div');deleteArea.className='document-delete-area';deleteArea.append(documentDeleteButton(doc));main.append(deleteArea)
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
 const ocr:ReturnType<typeof ocrControls>|null=doc.purpose==='photo'?null:ocrControls(signal=>api('/api/capabilities','GET',undefined,undefined,undefined,signal),{model:t('ocrModel'),languages:t('ocrLanguages'),quality:t('ocrQuality'),chooseModel:t('chooseModel'),chooseQuality:t('chooseQuality'),loading:t('loading'),unavailable:t('ocrUnavailable'),retry:t('retry'),external:t('ocrExternal'),presets:{auto:t('automatic'),standard:t('standardQuality'),fast:t('fastQuality'),accurate:t('accurateQuality')}},locale,()=>{submit.disabled=pending||!ocr!.valid()});
 const close=button('close',()=>{if(!pending)dialog.close()});
 const submit=button(doc.purpose==='photo'?'saveImage':'createPDF',async()=>{
  if(pending||ocr&&!ocr.valid())return;
  const command=doc.purpose==='photo'?{format:select.value,ocr:{enabled:false}}:{format:'pdf',pdf_encoding:select.value,ocr:ocr!.command()};
  const digest=JSON.stringify(command);if(digest!==fingerprint){fingerprint=digest;key=requestKey()}
  pending=true;submit.disabled=true;select.disabled=true;close.disabled=true;ocr?.lock(true);hint.textContent='';
  const controller=new AbortController();requestController=controller;const timeout=setTimeout(()=>controller.abort(),15000);
  try{const job=await api('/api/documents/'+doc.id+'/render','POST',command,key,doc.revision,controller.signal);if(!opaque(job.id))throw new Error('Invalid job');if(view!==generation||!dialog.isConnected)return;dialog.close();navigate('#/job/'+job.id)}
  catch(err){if(view===generation&&dialog.isConnected)hint.textContent=t(err instanceof APIError&&err.status===412?'changed':err instanceof APIError&&err.status===422?'unsupported':'failed')}
  finally{clearTimeout(timeout);pending=false;submit.disabled=ocr?!ocr.valid():false;select.disabled=false;close.disabled=false;ocr?.lock(false)}
 });submit.className='primary';
 const actions=document.createElement('div');actions.className='output-actions';actions.append(close,submit);const body=document.createElement('div');body.className='output-body';body.append(label,select);if(ocr){dialog.classList.add('with-ocr');body.append(ocr.element)}body.append(hint);dialog.append(title,body,actions);main.append(dialog);
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

document.addEventListener('visibilitychange',()=>{if(document.hidden)stopJob();else if(location.hash.startsWith('#/job/'))void render()});
