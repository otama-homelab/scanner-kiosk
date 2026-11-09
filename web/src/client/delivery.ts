import {requestKey} from './request-key';
import type {Key} from './messages';
type Options={doc:any;name:string;t:(key:Key)=>string;api:(path:string,method?:string,body?:unknown,key?:string,revision?:number,signal?:AbortSignal)=>Promise<any>;current:()=>boolean};
const states=new Map<string,Key>([['queued','deliveryQueued'],['attempted','deliveryAttempted'],['accepted','deliveryAccepted'],['confirmed','deliveryConfirmed'],['failed','deliveryFailed'],['unknown','deliveryUnknown'],['cancelled','deliveryCancelled']]);
const opaque=(id:unknown):id is string=>typeof id==='string'&&/^[A-Za-z0-9_-]{16,96}$/.test(id);
export function deliveryControl(o:Options){
 const {doc,t}=o,element=document.createElement('section');element.hidden=true;element.className='ocr-results';
 const controller=new AbortController();let disposed=false,pending=false,checked=false,uncertain=false,record:any=null,cap:any=null,timer:ReturnType<typeof setTimeout>|undefined,dialog:HTMLDialogElement|null=null,polls=0;
 const current=()=>!disposed&&o.current();
 const call=(path:string,method='GET',body?:unknown,key?:string,revision?:number)=>o.api(path,method,body,key,revision,AbortSignal.any([controller.signal,AbortSignal.timeout(15000)]));
 const make=(tag:string,value:string)=>{const node=document.createElement(tag);node.textContent=value;return node};
 const button=(key:Key,action:()=>void)=>{const b=document.createElement('button');b.type='button';b.textContent=t(key);b.disabled=pending;b.addEventListener('click',action);return b};
 const state=make('p','');state.setAttribute('role','status');state.tabIndex=-1;
 const intent='scanner.delivery.'+doc.id+'.'+doc.revision;
 try{uncertain=localStorage.getItem(intent)!==null}catch{uncertain=true}
 function validate(value:any){if(!opaque(value?.id)||value.document_id!==doc.id||!Number.isSafeInteger(value.revision)||value.destination!=='paperless'||!states.has(value.state))throw new Error('Invalid delivery');return value}
 async function read(){checked=false;try{record=validate(await call('/api/documents/'+doc.id+'/deliveries/current'));uncertain=false;try{localStorage.removeItem(intent)}catch{}checked=true}catch(err){if((err as {status?:number}).status===404){record=null;checked=true}else throw err}}
 function schedule(){if(timer)clearTimeout(timer);if(record&&['queued','attempted','accepted'].includes(record.state)&&polls++<40)timer=setTimeout(()=>void update(),1500)}
 async function update(){try{await read();if(current()){draw();schedule()}}catch{if(current()){checked=false;draw()}}}
 async function command(path:string,body:unknown){if(pending)return;pending=true;draw();try{record=validate(await call(path,'POST',body));if(current()){draw();state.focus();schedule()}}catch{if(current()){state.textContent=t('deliveryUnavailable');state.focus()}}finally{pending=false;if(current())draw()}}
 function confirm(opener:HTMLButtonElement){
  if(dialog||pending||!checked||uncertain||record)return;
  const key=requestKey();dialog=document.createElement('dialog');dialog.className='output-settings';dialog.setAttribute('aria-labelledby','delivery-confirm-title');
  const title=make('h2',t('deliverySend'));title.id='delivery-confirm-title';const hint=make('p',t('deliveryConfirmHelp'));
  const cancel=button('cancel',()=>{if(!pending)dialog?.close()});
  const submit=button('deliveryConfirm',()=>void(async()=>{
   if(pending)return;
   try{localStorage.setItem(intent,key)}catch{hint.textContent=t('deliveryUnavailable');return}
   uncertain=true;pending=true;submit.disabled=cancel.disabled=true;
   try{record=validate(await call('/api/documents/'+doc.id+'/deliveries','POST',{destination:'paperless',confirmed:true},key,doc.revision));checked=true;uncertain=false;localStorage.removeItem(intent)}
   catch{try{await read()}catch{checked=false}}
   finally{pending=false;if(current()){dialog?.close();draw();state.focus();schedule()}}
  })());submit.className='primary';const actions=document.createElement('div');actions.className='output-actions';actions.append(cancel,submit);
  dialog.append(title,make('p',o.name),make('p',cap.processing_destination),hint,actions);element.append(dialog);
  dialog.addEventListener('cancel',e=>{if(pending)e.preventDefault()});dialog.addEventListener('close',()=>{dialog?.remove();dialog=null;if(current()&&opener.isConnected)opener.focus()},{once:true});dialog.showModal();cancel.focus();
 }
 function draw(){
  if(!current()||!cap?.enabled)return;element.hidden=false;element.dataset.delivery='';
  const focused=element.contains(document.activeElement)?document.activeElement as HTMLElement:null;
  const focusKey=focused?.dataset.deliveryAction;
  const heading=make('h2','Paperless');const destination=make('p',t('deliveryDestination')+': '+cap.processing_destination);
  const health=make('p',t(!cap.configured?'deliveryUnconfigured':cap.health?.status==='healthy'?'deliveryHealthy':cap.health?.status==='unknown'?'deliveryHealthUnknown':'deliveryHealthFailed'));
  if(cap.health?.checked_at)health.append(document.createTextNode(' · '+new Date(cap.health.checked_at).toLocaleString()));
  const controls=document.createElement('div');controls.className='output-actions';element.replaceChildren(heading,destination,health,state,controls);
  const add=(key:Key,action:()=>void)=>{const b=button(key,action);b.dataset.deliveryAction=key;controls.append(b);return b};
  element.dataset.deliveryState=record?.state||'';
  state.textContent=record?t(states.get(record.state)!):uncertain?t('deliveryUnknown'):!checked?t('deliveryUnavailable'):'';
  if(record?.remote_processing_may_continue)element.append(make('p',t('deliveryRemoteContinues')));
  if(cap.configured){add('deliveryCheckHealth',()=>void(async()=>{if(pending)return;pending=true;draw();try{cap=await call('/api/capabilities/paperless/health','POST',{})}catch{cap.health={status:'unavailable'}}finally{pending=false;if(current())draw()}})());
   if(checked&&!record&&!uncertain){const send=add('deliverySend',()=>confirm(send))}
   if(!checked||uncertain)add('deliveryCheck',()=>void update());
  }
  if(record&&['queued','attempted','accepted'].includes(record.state))add('deliveryStop',()=>void command('/api/deliveries/'+record.id+'/cancel',{}));
  if(record&&['attempted','accepted','unknown'].includes(record.state)){
   const label=make('label',t('deliveryTask'));label.htmlFor='delivery-task';const input=document.createElement('input');input.id='delivery-task';input.type='text';input.maxLength=36;input.autocomplete='off';input.disabled=pending;input.value='';
   const form=document.createElement('form');form.className='output-body';const reconcile=button('deliveryReconcile',()=>{});reconcile.type='submit';form.append(label,input,make('p',t('deliveryRecoveryHelp')),reconcile);
   form.addEventListener('submit',event=>{event.preventDefault();if(pending)return;const value=input.value;if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)){input.setCustomValidity(t('deliveryTask'));input.reportValidity();return}void command('/api/deliveries/'+record.id+'/reconcile',{task_id:value})});input.addEventListener('input',()=>input.setCustomValidity(''));element.append(form);
  }
  if(focusKey)element.querySelector<HTMLElement>('[data-delivery-action="'+focusKey+'"]')?.focus();
 }
 void(async()=>{try{cap=(await call('/api/capabilities')).paperless;if(!current()||!cap?.enabled)return;if(typeof cap.processing_destination!=='string'||cap.processing_destination.length>512)throw new Error('Invalid destination');if(cap.configured)await read();else checked=true;if(current()){draw();schedule()}}catch{if(current()&&cap?.enabled){checked=false;draw()}}})();
 return {element,dispose(){disposed=true;controller.abort();if(timer)clearTimeout(timer);dialog?.remove()}};
}
