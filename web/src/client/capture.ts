import {requestKey} from './request-key';
export type CaptureIntent={purpose:'document'|'photo';photo_format:'jpeg'|'png'};
type Capture={size:string;color:string;dpi:number};
type Device={id:string;name:string;state:'idle'|'busy'|'unknown'|'unavailable';capture_options:Capture[];default_capture:Capture|null};
export type CaptureKey='saveSettings'|'fromImages'|'insertScan'|'replaceScan'|'requestExpired'|'reloadView'|'scanner'|'type'|'document'|'photo'|'chooseScanner'|'changeScanner'|'searchScanners'|'noScanners'|'noMatches'|'scannerIdle'|'scannerBusy'|'scannerUnknown'|'unavailable'|'scanSettings'|'size'|'color'|'gray'|'dpi'|'advanced'|'format'|'outputFormat'|'jpeg'|'png'|'close'|'apply'|'scan'|'scanNext'|'loading'|'retry'|'history'|'settingsChanged'|'scanRejected'|'scanUncertain'|'scanning'|'a4'|'a5'|'a6'|'letter'|'photo10x15';
let previousCapture:{scannerID:string;capture:Capture}|null=null;
type Options={initialCapture?:Capture;submitLabel?:'insertScan'|'replaceScan'|'scan';t:(key:CaptureKey)=>string;current:()=>boolean;read:(signal:AbortSignal)=>Promise<unknown>;submit:(body:unknown,key:string,signal:AbortSignal)=>Promise<any>;accepted:(job:any)=>void;history:()=>void;rejected:(error:unknown)=>boolean;expired:(error:unknown)=>boolean;reload:()=>void;purpose?:'document'|'photo';photoFormat?:'jpeg'|'png'};
const same=(a:Capture|null,b:Capture|null)=>!!a&&!!b&&a.size===b.size&&a.color===b.color&&a.dpi===b.dpi;
const opaque=(id:unknown):id is string=>typeof id==='string'&&/^[A-Za-z0-9_-]{16,96}$/.test(id);
function inventory(raw:any):Device[]{
 if(!Array.isArray(raw?.scanners)||raw.scanners.length>32)throw new Error('Invalid scanner list');const seen=new Set<string>();
 return raw.scanners.map((device:any)=>{
  if(!opaque(device.id)||seen.has(device.id)||typeof device.name!=='string'||!device.name.length||[...device.name].length>256||!['idle','busy','unknown','unavailable'].includes(device.state)||!Array.isArray(device.capture_options)||device.capture_options.length>60)throw new Error('Invalid scanner');seen.add(device.id);
  const choices=new Set<string>();for(const c of device.capture_options){if(!['a4','a5','a6','letter','photo10x15'].includes(c?.size)||!['color','gray'].includes(c?.color)||![75,100,150,200,300,600].includes(c?.dpi)||choices.has(JSON.stringify([c.size,c.color,c.dpi])))throw new Error('Invalid capture');choices.add(JSON.stringify([c.size,c.color,c.dpi]))}
  if(device.default_capture!==null&&!device.capture_options.some((c:Capture)=>same(c,device.default_capture)))throw new Error('Invalid default');return device as Device;
 });
}
export function captureControls(options:Options){
 const home=!options.purpose,t=options.t,element=document.createElement('div');element.className='home-panels capture-panels'+(home?' capture-intake':'');
 const node=(tag:string,value='')=>{const e=document.createElement(tag);e.textContent=value;return e};
 const action=(key:CaptureKey,fn:()=>void)=>{const b=document.createElement('button');b.type='button';b.textContent=t(key);b.addEventListener('click',fn);return b};
 const devicePanel=node('section');devicePanel.className='panel capture-device';devicePanel.append(node('h2',t('scanner')));
 const identity=node('p');identity.className='capture-identity';const state=node('p');state.className='capture-state';state.setAttribute('role','status');
 const choose=action('chooseScanner',()=>scannerDialog());const refresh=action('retry',()=>void read(true));
 const sourceActions=node('div');sourceActions.className='capture-source-actions';devicePanel.append(identity,state,choose,refresh);const imagePanel=node('section');imagePanel.className='panel capture-import';imagePanel.append(node('h2',t('fromImages')),sourceActions);
 const settings=node('section');settings.className='panel capture-settings';settings.append(node('h2',t(home?'saveSettings':'type')));
 const purposes=node('div');purposes.className='capture-purposes';purposes.setAttribute('role','group');purposes.setAttribute('aria-label',t('type'));let purpose:CaptureIntent['purpose']=options.purpose||'document';
 const purposeButtons:HTMLButtonElement[]=[];
 for(const kind of ['document','photo'] as const){const b=action(kind,()=>{if(pending||importLocked||options.purpose)return;purpose=kind;update()});b.setAttribute('aria-pressed',String(purpose===kind));b.disabled=!!options.purpose;purposeButtons.push(b);purposes.append(b)}
 const scanSummary=node('dl');scanSummary.className='capture-summary capture-scan-summary';const summary=node('dl');summary.className='capture-summary';const summaryValues:HTMLElement[]=[];
 for(const key of ['size','color','dpi','outputFormat'] as const){const field=node('div'),label=node('dt',t(key)),value=node('dd');if(key==='outputFormat')label.id='capture-output-format-label';field.append(label,value);(home&&key!=='outputFormat'?scanSummary:summary).append(field);summaryValues.push(value)}
 const encoding=document.createElement('select');encoding.id='capture-photo-format';encoding.setAttribute('aria-labelledby','capture-output-format-label');for(const value of ['jpeg','png']){const option=document.createElement('option');option.value=value;option.textContent=t(value as CaptureKey);encoding.append(option)}encoding.addEventListener('change',()=>{if(!pending&&!importLocked){format=encoding.value==='png'?'png':'jpeg';update()}});
 const edit=action('scanSettings',()=>settingsDialog());
 const notice=node('p');notice.className='capture-notice';notice.setAttribute('role','status');
 const history=action('history',options.history);history.hidden=true;
 const start=action(options.submitLabel||(options.purpose?'scanNext':'scan'),()=>void submit());start.className='primary capture-scan-start';
 const reload=action('reloadView',options.reload);reload.hidden=true;settings.append(purposes,summary);if(home){devicePanel.append(scanSummary,edit,notice,start,history,reload);element.append(settings,devicePanel,imagePanel)}else{settings.append(edit,notice,start,history,reload);element.append(devicePanel,settings)};
 let devices:Device[]=[],selected='',capture:Capture|null=null,format:CaptureIntent['photo_format']=options.photoFormat||'jpeg',loading=true,loaded=false,available=false,pending=false,uncertain=false,expired=false,disposed=false,manualRefresh=false,importLocked=false;
 let timer:ReturnType<typeof setTimeout>|undefined,readController:AbortController|null=null,submitController:AbortController|null=null,modal:HTMLDialogElement|null=null;
 let fingerprint='',key='';
 const setText=(e:HTMLElement,value:string)=>{if(e.textContent!==value)e.textContent=value};
 const active=()=>!disposed&&options.current();const device=()=>devices.find(d=>d.id===selected);
 const valid=()=>!!capture&&!!device()?.capture_options.some(c=>same(c,capture));
 const statusKey=(value:Device['state']):CaptureKey=>value==='idle'?'scannerIdle':value==='busy'?'scannerBusy':value==='unavailable'?'unavailable':'scannerUnknown';
 function update(){
  const current=device();setText(identity,current?.name||t(devices.length?'chooseScanner':'noScanners'));setText(state,t(loading?'loading':!available?'unavailable':current?statusKey(current.state):'scannerUnknown'));
  choose.textContent=t(home?'chooseScanner':current?'changeScanner':'chooseScanner');choose.hidden=!home&&(devices.length===0||devices.length===1&&!!current);choose.disabled=pending||!devices.length;refresh.disabled=pending||!loaded||manualRefresh;refresh.hidden=!loaded||available&&devices.length>0&&(!current||current.state==='idle'||current.state==='busy');
  for(let i=0;i<purposeButtons.length;i++){purposeButtons[i].setAttribute('aria-pressed',String(purpose===(i===0?'document':'photo')));purposeButtons[i].disabled=pending||importLocked||!!options.purpose}
  for(const [i,value] of [capture?t(capture.size as CaptureKey):'—',capture?t(capture.color as CaptureKey):'—',capture?String(capture.dpi):'—',purpose==='photo'?t(format as CaptureKey):'PDF'].entries()){
   const field=summaryValues[i];if(i===3&&purpose==='photo'&&!options.purpose){if(encoding.parentElement!==field)field.replaceChildren(encoding);encoding.value=format;encoding.disabled=pending||importLocked}else setText(field,value)
  }
  edit.disabled=pending||importLocked||!current?.capture_options.length;start.disabled=pending||importLocked||uncertain||expired||loading||!available||current?.state!=='idle'||!valid();setText(start,t(pending?'scanning':options.submitLabel||(options.purpose?'scanNext':'scan')));
  history.hidden=!uncertain;reload.hidden=!expired;
 }
 function select(id:string){const chosen=devices.find(d=>d.id===id);if(!chosen)return;selected=id;capture=chosen.default_capture?{...chosen.default_capture}:chosen.capture_options[0]?{...chosen.capture_options[0]}:null;notice.textContent='';update()}
 async function read(manual=false){
  if(!active()||pending||document.hidden||readController)return;
  const controller=new AbortController();readController=controller;manualRefresh=manual;const timeout=setTimeout(()=>controller.abort(),10000);loading=!available;update();
  try{const result=inventory(await options.read(controller.signal));if(!active()||controller.signal.aborted)return;devices=result;available=true;if(!selected&&devices.length===1)select(devices[0].id);if(selected&&!capture&&device()?.capture_options.length)capture={...(device()!.default_capture||device()!.capture_options[0])};if(!uncertain&&selected&&!valid()&&capture)notice.textContent=t('settingsChanged')}
  catch{if(active()&&readController===controller&&!pending&&!document.hidden)available=false}
  finally{clearTimeout(timeout);if(active()&&readController===controller){loaded=true;loading=false;manualRefresh=false;readController=null;update();if(timer)clearTimeout(timer);timer=setTimeout(()=>void read(),5000)}}
 }
 function dialog(title:CaptureKey){
  if(modal||pending)return null;const d=document.createElement('dialog');d.className='capture-dialog';const h=node('h2',t(title));h.id='capture-dialog-title';d.setAttribute('aria-labelledby',h.id);const body=node('div');body.className='capture-dialog-body';const actions=node('div');actions.className='capture-dialog-actions';actions.append(action('close',()=>d.close()));d.append(h,body,actions);element.append(d);modal=d;
  d.addEventListener('click',event=>{const box=d.getBoundingClientRect();if(event.target===d&&(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom))d.close()});d.addEventListener('close',()=>{d.remove();if(modal===d)modal=null},{once:true});d.showModal();return{d,body,actions};
 }
 function scannerDialog(){
  const view=dialog('chooseScanner');if(!view)return;const label=node('label',t('searchScanners'));const search=document.createElement('input');search.type='search';search.id='scanner-search';label.setAttribute('for',search.id);const list=node('div');list.className='scanner-results';view.body.append(label,search,list);
  const draw=()=>{const query=search.value.toLocaleLowerCase().trim();list.replaceChildren();for(const item of devices.filter(d=>d.name.toLocaleLowerCase().includes(query))){const b=action('scanner',()=>{select(item.id);view.d.close()});b.className='scanner-result';b.setAttribute('aria-pressed',String(selected===item.id));b.replaceChildren(node('strong',item.name),node('span',t(statusKey(item.state))));list.append(b)}if(!list.childElementCount)list.append(node('p',t('noMatches')))};search.addEventListener('input',draw);draw();search.focus();
 }
 function settingsDialog(){
  const current=device();if(!current?.capture_options.length)return;const view=dialog('scanSettings');if(!view)return;const choices=current.capture_options;let draft=valid()?{...capture!}:{...(current.default_capture||choices[0])};let draftFormat=format;
  const control=(key:CaptureKey)=>{const label=node('label',t(key));const select=document.createElement('select');select.id='capture-'+key;label.setAttribute('for',select.id);view.body.append(label,select);return select};
  const size=control('size'),color=control('color');const advanced=document.createElement('details');const legend=node('summary',t('advanced'));const dpi=document.createElement('select');dpi.id='capture-dpi';const dpiLabel=node('label',t('dpi'));dpiLabel.setAttribute('for',dpi.id);advanced.append(legend,dpiLabel,dpi);
  const encoding=purpose==='photo'&&!options.purpose?control('format'):null;if(encoding){for(const value of ['jpeg','png']){const option=document.createElement('option');option.value=value;option.textContent=t(value as CaptureKey);encoding.append(option)}encoding.value=draftFormat;encoding.addEventListener('change',()=>draftFormat=encoding.value==='png'?'png':'jpeg')}
  view.body.append(advanced);
  const fill=(select:HTMLSelectElement,values:(string|number)[],selected:string|number,labels:boolean)=>{select.replaceChildren();for(const value of [...new Set(values)]){const option=document.createElement('option');option.value=String(value);option.textContent=labels?t(String(value) as CaptureKey):String(value);select.append(option)}select.value=String(selected)};
  const draw=()=>{fill(size,choices.map(c=>c.size),draft.size,true);fill(color,choices.filter(c=>c.size===draft.size).map(c=>c.color),draft.color,true);fill(dpi,choices.filter(c=>c.size===draft.size&&c.color===draft.color).map(c=>c.dpi),draft.dpi,false)};
  size.addEventListener('change',()=>{draft={...(choices.find(c=>c.size===size.value&&c.color===draft.color&&c.dpi===draft.dpi)||choices.find(c=>c.size===size.value)!) };draw()});color.addEventListener('change',()=>{draft={...(choices.find(c=>c.size===draft.size&&c.color===color.value&&c.dpi===draft.dpi)||choices.find(c=>c.size===draft.size&&c.color===color.value)!)};draw()});dpi.addEventListener('change',()=>draft={...draft,dpi:Number(dpi.value)});draw();
  const apply=action('apply',()=>{if(!device()?.capture_options.some(c=>same(c,draft))){notice.textContent=t('settingsChanged');view.d.close();update();return}capture=draft;format=draftFormat;notice.textContent='';view.d.close();update()});apply.className='primary';view.actions.append(apply);
 }
 async function submit(){
  if(start.disabled||!active()||!capture||modal)return;
  const command=options.purpose?{scanner_id:selected,capture:{...capture}}:{scanner_id:selected,purpose,capture:{...capture},...(purpose==='photo'?{photo_format:format}:{})};const digest=JSON.stringify(command);if(fingerprint!==digest){fingerprint=digest;key=requestKey()}
  pending=true;readController?.abort();if(timer)clearTimeout(timer);notice.textContent='';update();const controller=new AbortController();submitController=controller;const timeout=setTimeout(()=>controller.abort(),15000);
  try{const job=await options.submit(command,key,controller.signal);if(!opaque(job?.id)||!opaque(job?.document_id)||job.kind!=='scan')throw new Error('Invalid admission');if(active()&&!controller.signal.aborted){previousCapture={scannerID:selected,capture:{...capture}};options.accepted(job)}}
  catch(err){if(active()){expired=options.expired(err);uncertain=!expired&&!options.rejected(err);notice.textContent=t(expired?'requestExpired':uncertain?'scanUncertain':'scanRejected')}}
  finally{clearTimeout(timeout);pending=false;submitController=null;if(active()){update();if(!uncertain)void read()}}
 }
 const visibility=()=>{if(document.hidden){available=false;readController?.abort();if(timer)clearTimeout(timer);update()}else void read()};document.addEventListener('visibilitychange',visibility);
 update();
 return {element,sourceActions,getImportIntent:():CaptureIntent=>({purpose,photo_format:format}),restoreImportIntent:(intent:CaptureIntent)=>{purpose=intent.purpose;format=intent.photo_format;update()},lockImportIntent:(locked:boolean)=>{if(importLocked!==locked){importLocked=locked;update()}},start:async(immediate=false)=>{if(immediate&&previousCapture){selected=previousCapture.scannerID;capture={...previousCapture.capture}}await read();if(immediate&&active()){if(options.initialCapture&&device()){capture={...options.initialCapture};if(!valid())notice.textContent=t('settingsChanged');update()}await submit()}},dispose:()=>{disposed=true;readController?.abort();submitController?.abort();if(timer)clearTimeout(timer);modal?.remove();modal=null;document.removeEventListener('visibilitychange',visibility)}};
}
