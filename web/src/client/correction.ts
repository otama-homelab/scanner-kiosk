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
 changed:(revision:number)=>void;
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
 };
};

const png='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2n1cAAAAASUVORK5CYII=';

export function correctionControl(options:Options){
 const button=document.createElement('button');button.type='button';button.textContent=options.labels.compare;button.dataset.pageAction='correction';
 let dialog:HTMLDialogElement|null=null,controller:AbortController|null=null,pending=false;
 const unsafe=options.correction.risk_flags.some(flag=>flag==='edge-risk'||flag==='low-confidence'||flag==='fallback');
 const close=()=>{if(!pending)dialog?.close()};
 const open=()=>{
  if(dialog||!options.current())return;
  const key=options.nextKey();
  dialog=document.createElement('dialog');dialog.className='correction-dialog';dialog.setAttribute('aria-labelledby','correction-title');
  const title=document.createElement('h2');title.id='correction-title';title.textContent=options.labels.title;
  const compare=document.createElement('div');compare.className='correction-compare';
  for(const [label,url] of [[options.labels.original,options.originalURL],[options.labels.suggested,options.correction.preview_url]] as const){
   const figure=document.createElement('figure');const caption=document.createElement('figcaption');caption.textContent=label;
   const image=document.createElement('img');image.alt=label;image.src=url||png;image.addEventListener('error',()=>{image.src=png},{once:true});figure.append(caption,image);compare.append(figure);
  }
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
  apply.disabled=unsafe||options.correction.state==='applied';
  const run=async(action:(signal:AbortSignal)=>Promise<{revision:number}>)=>{
   if(pending||!options.current())return;pending=true;apply.disabled=true;keep.disabled=true;controller=new AbortController();
   try{const result=await action(controller.signal);if(options.current()){dialog?.close();options.changed(result.revision)}}catch(error){const result=options.classify(error);notice.textContent=result.message;if(result.unknown){apply.disabled=true;keep.disabled=false}else{apply.disabled=unsafe||options.correction.state==='applied'}}finally{controller=null;pending=false;if(dialog?.open)keep.disabled=false}
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
  dialog.append(title,compare,details,notice,actions);
  dialog.addEventListener('close',()=>{controller?.abort();dialog=null},{once:true});
  dialog.addEventListener('cancel',event=>{if(pending)event.preventDefault()});
  document.body.append(dialog);dialog.showModal();keep.focus();
 };
 button.addEventListener('click',open);
 return {button,element:button,dispose:()=>{controller?.abort();dialog?.remove();dialog=null}};
}
