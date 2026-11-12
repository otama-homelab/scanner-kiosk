import type {Key} from './messages';
type CorrectionView={
 suggestion_id:string;
 correction_artifact_id:string;
 source_sha256:string;
 correction_sha256:string;
 preview_url:string;
 transform:{output_width:number;output_height:number;rotation_degrees:number};
 confidence_kind:string;
 confidence_value?:number;
 risk_flags:string[];
 state:'suggested'|'applied';
 can_undo:boolean;
};

type Options={
 documentID:string;
 pageID:string;
 originalURL:string;
 revision:number;
 correction:CorrectionView;
 current:()=>boolean;
 nextKey:()=>string;
 apply:(suggestion:string,source:string,key:string,signal:AbortSignal)=>Promise<{revision:number}>;
 undo:(key:string,signal:AbortSignal)=>Promise<{revision:number}>;
 changed:(revision:number)=>Promise<void>;
 classify:(error:unknown)=>{message:string;unknown:boolean};
 labels:{
  compare:string;
  title:string;
  original:string;
  suggested:string;
  apply:string;
  keep:string;
  undo:string;
  reprocess:string;
  unsafe:string;
  transform:string;
  confidence:string;
  unavailable:string;
  previewUnavailable:string;
 };
};

export function correctionControl(options:Options){
 const button=document.createElement('button');button.type='button';button.textContent=options.labels.compare;button.dataset.pageAction='correction';
 let dialog:HTMLDialogElement|null=null,controller:AbortController|null=null,pending=false;
 const unsafe=options.correction.risk_flags.length>0;
 const close=()=>{if(!pending)dialog?.close()};
 const focusReplacement=()=>{
  const card=Array.from(document.querySelectorAll<HTMLElement>('[data-page-id]')).find(element=>element.dataset.pageId===options.pageID);
  const replacement=card?.querySelector<HTMLButtonElement>('[data-page-action="correction"]');
  if(replacement){replacement.focus();return}
  const heading=document.querySelector<HTMLElement>('h1[data-document-title]');if(heading){heading.tabIndex=-1;heading.focus()}
 };
 const open=()=>{
  if(dialog||!options.current())return;
  const key=options.nextKey();
  const currentDialog=document.createElement('dialog');currentDialog.className='correction-dialog';currentDialog.setAttribute('aria-labelledby','correction-title');dialog=currentDialog;let restoreFocus=true;
  const title=document.createElement('h2');title.id='correction-title';title.textContent=options.labels.title;
  const compare=document.createElement('div');compare.className='correction-compare';
  let originalState:'pending'|'loaded'|'failed'='pending',correctionState:'pending'|'loaded'|'failed'='pending';
  const details=document.createElement('dl');details.className='correction-details';
  const values=[
   [options.labels.transform,`${options.correction.transform.output_width}×${options.correction.transform.output_height}, ${options.correction.transform.rotation_degrees}°`],
   [options.labels.confidence,options.correction.confidence_value==null?options.labels.unavailable:String(options.correction.confidence_value)],
  ];
  for(const [label,value] of values){const row=document.createElement('div');const dt=document.createElement('dt'),dd=document.createElement('dd');dt.textContent=label;dd.textContent=value;row.append(dt,dd);details.append(row)}
  const notice=document.createElement('p');notice.className='correction-notice';notice.setAttribute('role','status');notice.textContent=unsafe?options.labels.unsafe:'';
  const actions=document.createElement('div');actions.className='correction-actions';
  const keep=document.createElement('button');keep.type='button';keep.textContent=options.labels.keep;keep.addEventListener('click',close);
  const apply=document.createElement('button');apply.type='button';apply.className='primary';apply.textContent=options.labels.apply;
  const refreshApply=()=>{apply.disabled=pending||unsafe||options.correction.state==='applied'||originalState!=='loaded'||correctionState!=='loaded'};
  const text=(tag:string,value:string)=>{const element=document.createElement(tag);element.textContent=value;return element};
  const previewUnavailable=(figure:HTMLElement)=>{
   figure.querySelector('img')?.remove();
   const unavailable=text('p',options.labels.previewUnavailable);unavailable.className='correction-preview-unavailable';unavailable.setAttribute('role','status');figure.append(unavailable);
   notice.textContent=options.labels.previewUnavailable;
  };
  for(const [kind,label,url] of [['original',options.labels.original,options.originalURL],['correction',options.labels.suggested,options.correction.preview_url]] as const){
   const figure=document.createElement('figure');const caption=document.createElement('figcaption');caption.textContent=label;
   const image=document.createElement('img');image.alt=label;figure.append(caption,image);compare.append(figure);
   const loaded=()=>{if(kind==='original')originalState='loaded';else correctionState='loaded';refreshApply()};
   const failed=()=>{if(kind==='original')originalState='failed';else correctionState='failed';previewUnavailable(figure);refreshApply()};
   image.addEventListener('load',loaded,{once:true});image.addEventListener('error',failed,{once:true});
   if(url)image.src=url;else failed();
  }
  refreshApply();
  const run=async(action:(signal:AbortSignal)=>Promise<{revision:number}>)=>{
   if(pending||!options.current())return;pending=true;refreshApply();keep.disabled=true;controller=new AbortController();
   try{const result=await action(controller.signal);if(options.current()){restoreFocus=false;currentDialog.close();await options.changed(result.revision);focusReplacement()}}catch(error){const result=options.classify(error);notice.textContent=result.message;if(result.unknown){apply.disabled=true;keep.disabled=false}else{refreshApply()}}finally{controller=null;pending=false;if(currentDialog.open)keep.disabled=false}
  };
  apply.addEventListener('click',()=>void run(signal=>options.apply(options.correction.suggestion_id,options.correction.source_sha256,key,signal)));
  actions.append(keep,apply);
  if(options.correction.state==='applied'){
   const undo=document.createElement('button');undo.type='button';undo.textContent=options.labels.undo;
   undo.addEventListener('click',()=>void run(signal=>options.undo(key,signal)));
   const reprocess=document.createElement('button');reprocess.type='button';reprocess.textContent=options.labels.reprocess;
   reprocess.addEventListener('click',()=>void run(signal=>options.undo(key,signal)));
   actions.append(undo,reprocess);
  }
  currentDialog.append(title,compare,details,notice,actions);
  currentDialog.addEventListener('close',()=>{controller?.abort();currentDialog.remove();if(dialog===currentDialog)dialog=null;if(restoreFocus&&button.isConnected)button.focus()},{once:true});
  currentDialog.addEventListener('cancel',event=>{if(pending)event.preventDefault()});
  document.body.append(currentDialog);currentDialog.showModal();keep.focus();
 };
 button.addEventListener('click',open);
 return {button,element:button,dispose:()=>{controller?.abort();dialog?.remove();dialog=null}};
}

export function photoCorrectionControl(options:{t:(key:Key)=>string;page:any;current:()=>boolean;apply:(crop:object,rotation:number,key:string,signal:AbortSignal)=>Promise<void>;undo:(key:string,signal:AbortSignal)=>Promise<void>;changed:()=>Promise<void>;nextKey:()=>string;classify:(error:unknown)=>{message:string;unknown:boolean}}){
 const {t,page}=options,button=document.createElement('button');button.type='button';button.textContent=t('photoCrop');button.dataset.photoCrop='true';let dialog:HTMLDialogElement|null=null,controller:AbortController|null=null;
 button.addEventListener('click',()=>{
  if(dialog||!options.current())return;controller=new AbortController();const signal=controller.signal;dialog=document.createElement('dialog');dialog.className='photo-crop';dialog.setAttribute('aria-labelledby','photo-crop-title');
  const title=document.createElement('h2');title.id='photo-crop-title';title.textContent=t('photoCrop');
  const help=document.createElement('p');help.textContent=t('photoCropHelp');const form=document.createElement('form');const fields=document.createElement('div');fields.className='photo-crop-fields';
  const inputs:HTMLInputElement[]=[];for(const [name,label,value,max] of [['x','cropLeft',0,page.photo.width-1],['y','cropTop',0,page.photo.height-1],['width','cropWidth',page.photo.width,page.photo.width],['height','cropHeight',page.photo.height,page.photo.height]] as const){const wrap=document.createElement('label');wrap.textContent=t(label);const input=document.createElement('input');input.type='number';input.name=name;input.min=name==='width'||name==='height'?'1':'0';input.max=String(max);input.step='1';input.value=String(value);input.required=true;wrap.append(input);fields.append(wrap);inputs.push(input)}
  const rotationLabel=document.createElement('label');rotationLabel.textContent=t('rotate');const rotation=document.createElement('select');rotation.setAttribute('aria-label',t('rotate'));for(const value of [0,90,180,270]){const option=document.createElement('option');option.value=String(value);option.textContent=value+'°';rotation.append(option)}rotation.value=String(page.rotation);rotationLabel.append(rotation);const precise=document.createElement('details');precise.className='crop-precise';const preciseTitle=document.createElement('summary');preciseTitle.textContent=t('cropPrecise');precise.append(preciseTitle,fields);
  const canvas=document.createElement('canvas');canvas.setAttribute('role','img');canvas.setAttribute('aria-label',t('preview'));canvas.hidden=true;
  const stage=document.createElement('div');stage.className='crop-stage';stage.style.width=`min(calc(100% - 2.75rem), ${50*page.photo.width/page.photo.height}dvh)`;
  const source=document.createElement('img');source.alt=t('preview');source.draggable=false;
  const selection=document.createElement('div');selection.className='crop-selection';
  const corners=document.createElement('div');corners.className='crop-corners';
  const handles:HTMLButtonElement[]=[];let activeCorner=0,drag:number|null=null,sourceReady=false;
  const chooseCorner=(index:number)=>{activeCorner=index;handles.forEach((handle,i)=>handle.setAttribute('aria-pressed',String(i===index)));Array.from(corners.querySelectorAll('button')).forEach((control,i)=>control.setAttribute('aria-pressed',String(i===index)))};
  for(const [index,label] of ['cropTopLeft','cropBottomRight'].entries()){
   const handle=document.createElement('button');handle.type='button';handle.className='crop-handle crop-handle-'+index;handle.setAttribute('aria-label',t(label as Key));handle.textContent=String(index+1);handles.push(handle);selection.append(handle);
   const choice=document.createElement('button');choice.type='button';choice.textContent=t(label as Key);choice.addEventListener('click',()=>chooseCorner(index));corners.append(choice);
   handle.addEventListener('pointerdown',event=>{if(pending||unknown||!sourceReady)return;event.preventDefault();chooseCorner(index);handle.focus();drag=event.pointerId;handle.setPointerCapture(event.pointerId)});
   handle.addEventListener('keydown',event=>{if(pending||unknown||!sourceReady||!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(event.key))return;event.preventDefault();chooseCorner(index);const [x,y,w,h]=inputs.map(input=>input.valueAsNumber),step=event.shiftKey?10:1;moveCorner((index?x+w:x)+(event.key==='ArrowRight'?step:event.key==='ArrowLeft'?-step:0),(index?y+h:y)+(event.key==='ArrowDown'?step:event.key==='ArrowUp'?-step:0))});
  }
  stage.append(source,selection);chooseCorner(0);
  const original=document.createElement('a');original.href=page.preview_url.replace(/\/preview$/,'/original');original.target='_blank';original.rel='noopener noreferrer';original.textContent=t('photoViewOriginal');
  const state=document.createElement('p');state.setAttribute('role','status');state.setAttribute('aria-live','polite');
  const actions=document.createElement('div');actions.className='photo-import-actions';const preview=document.createElement('button');preview.type='submit';preview.textContent=t('photoCropPreview');const cancel=document.createElement('button');cancel.type='button';cancel.textContent=t('cancel');cancel.addEventListener('click',()=>dialog?.close());const apply=document.createElement('button');apply.type='button';apply.textContent=t('photoCropApply');apply.className='primary';apply.disabled=true;actions.append(preview,cancel,apply);
  const undo=document.createElement('button');undo.type='button';undo.textContent=t('photoCropUndo');undo.hidden=page.correction?.state!=='applied';actions.append(undo);
  let pending=false,unknown=false,lastUndo=false,key=options.nextKey(),ready=false,previewVersion=0;const image=new Image();image.src=page.preview_url.replace(/\/preview$/,'/photo-baseline');
  const syncSelection=()=>{const [x,y,w,h]=inputs.map(input=>input.valueAsNumber),valid=[x,y,w,h].every(Number.isSafeInteger)&&x>=0&&y>=0&&w>0&&h>0&&x+w<=page.photo.width&&y+h<=page.photo.height;selection.hidden=!valid||!sourceReady;if(valid){selection.style.left=100*x/page.photo.width+'%';selection.style.top=100*y/page.photo.height+'%';selection.style.width=100*w/page.photo.width+'%';selection.style.height=100*h/page.photo.height+'%'}};
  const moveCorner=(left:number,top:number)=>{const [x,y,w,h]=inputs.map(input=>input.valueAsNumber);if(![x,y,w,h].every(Number.isSafeInteger))return;const right=x+w,bottom=y+h;const a=activeCorner?x:Math.max(0,Math.min(right-1,Math.round(left))),b=activeCorner?y:Math.max(0,Math.min(bottom-1,Math.round(top)));const c=activeCorner?Math.max(x+1,Math.min(page.photo.width,Math.round(left))):right,d=activeCorner?Math.max(y+1,Math.min(page.photo.height,Math.round(top))):bottom;[a,b,c-a,d-b].forEach((value,index)=>inputs[index].value=String(value));invalidate()};
  const movePointer=(event:PointerEvent)=>{const box=source.getBoundingClientRect();if(box.width&&box.height)moveCorner((event.clientX-box.left)*page.photo.width/box.width,(event.clientY-box.top)*page.photo.height/box.height)};
  stage.addEventListener('pointerdown',event=>{if(pending||unknown||!sourceReady||event.target!==source)return;movePointer(event)});
  stage.addEventListener('pointermove',event=>{if(drag===event.pointerId&&!pending&&!unknown)movePointer(event)});
  for(const name of ['pointerup','pointercancel','lostpointercapture'])stage.addEventListener(name,()=>{drag=null});
  source.addEventListener('load',()=>{sourceReady=true;syncSelection()},{once:true});source.addEventListener('error',()=>{sourceReady=false;selection.hidden=true;state.textContent=t('correctionPreviewUnavailable')},{once:true});source.src=image.src;
  const draw=()=>{const [x,y,w,h]=inputs.map(i=>i.valueAsNumber),angle=Number(rotation.value),sideways=angle%180!==0;canvas.width=sideways?h:w;canvas.height=sideways?w:h;const ctx=canvas.getContext('2d')!;ctx.translate(canvas.width/2,canvas.height/2);ctx.rotate(angle*Math.PI/180);ctx.drawImage(image,x,y,w,h,-w/2,-h/2,w,h)};
  const invalidate=()=>{previewVersion++;ready=false;apply.disabled=true;canvas.hidden=true;syncSelection()};for(const input of inputs)input.addEventListener('input',invalidate);rotation.addEventListener('change',()=>{if(ready)draw()});
  form.addEventListener('submit',async event=>{event.preventDefault();const version=++previewVersion;if(pending||unknown)return;const [x,y,w,h]=inputs.map(i=>i.valueAsNumber);if(!form.reportValidity()||!Number.isSafeInteger(x+y+w+h)||x+w>page.photo.width||y+h>page.photo.height){state.textContent=t('photoCropInvalid');return}try{await image.decode();if(!dialog?.isConnected||signal.aborted||version!==previewVersion)return;draw();canvas.hidden=false;ready=true;apply.disabled=false;state.textContent=t('photoCropReady')}catch{if(!signal.aborted)state.textContent=t('correctionPreviewUnavailable')}});
  const run=async(isUndo:boolean)=>{if(pending||!isUndo&&!ready&&!unknown)return;if(unknown)isUndo=lastUndo;else lastUndo=isUndo;pending=true;const route=location.hash;for(const input of inputs)input.disabled=true;rotation.disabled=preview.disabled=apply.disabled=undo.disabled=true;state.textContent=t('loading');try{if(isUndo)await options.undo(key,signal);else{const [x,y,width,height]=inputs.map(i=>i.valueAsNumber);await options.apply({page_id:page.id,source_sha256:page.photo.sha256,x,y,width,height},Number(rotation.value),key,signal)}if(!options.current()||signal.aborted)return;dialog?.close();await options.changed();if(location.hash===route)document.querySelector<HTMLButtonElement>(`[data-page-id="${page.id}"] [data-photo-crop]`)?.focus()}catch(error){if(signal.aborted)return;const result=options.classify(error);unknown=result.unknown;state.textContent=result.message;if(unknown)apply.textContent=t('checkImport');else key=options.nextKey()}finally{pending=false;if(dialog?.isConnected){for(const input of inputs)input.disabled=unknown;rotation.disabled=preview.disabled=unknown;apply.disabled=!ready&&!unknown;undo.disabled=unknown}}};apply.addEventListener('click',()=>void run(false));undo.addEventListener('click',()=>void run(true));
  form.append(stage,corners,rotationLabel,precise,canvas,state,actions);dialog.append(title,help,original,form);document.body.append(dialog);dialog.addEventListener('close',()=>{controller?.abort();dialog?.remove();dialog=null;if(button.isConnected)button.focus()},{once:true});dialog.showModal();handles[0].focus();
 });
 return {button,dispose(){controller?.abort();dialog?.remove();dialog=null}};
}
