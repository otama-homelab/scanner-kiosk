// The server owns deletion and cleanup. This dialog retains only one command's
// key/pending request; an uncertain response is retried with the exact same key.
export function deletionDialog(options:{
 root:HTMLElement;opener:HTMLButtonElement;current:()=>boolean;
 labels:{title:string;description:string;cancel:string;remove:string;retry:string;reload:string;changed:string;expired:string;failed:string;uncertain:string};
 request:(signal:AbortSignal)=>Promise<unknown>;accepted:(value:unknown)=>void;
 classify:(error:unknown)=>'changed'|'expired'|'failed'|'uncertain';reload:()=>void;closed:()=>void;
}){
 const {labels}=options;const dialog=document.createElement('dialog');dialog.className='deletion-dialog';
 dialog.setAttribute('aria-labelledby','delete-document-title');dialog.setAttribute('aria-describedby','delete-document-description');
 const title=document.createElement('h2');title.id='delete-document-title';title.textContent=labels.title;
 const description=document.createElement('p');description.id='delete-document-description';description.textContent=labels.description;
 const hint=document.createElement('p');hint.setAttribute('role','alert');
 const actions=document.createElement('div');actions.className='deletion-actions';
 const cancel=document.createElement('button');cancel.type='button';cancel.textContent=labels.cancel;
 const submit=document.createElement('button');submit.type='button';submit.textContent=labels.remove;submit.className='danger';
 const reload=document.createElement('button');reload.type='button';reload.textContent=labels.reload;reload.hidden=true;
 let pending=false,disposed=false;let controller:AbortController|null=null;let timer:ReturnType<typeof setTimeout>|undefined;
 const dispose=()=>{if(disposed)return;disposed=true;controller?.abort();if(timer)clearTimeout(timer);dialog.remove();options.closed();if(options.opener.isConnected)options.opener.focus()};
 cancel.addEventListener('click',()=>dialog.close());reload.addEventListener('click',()=>{dialog.close();options.reload()});
 submit.addEventListener('click',async()=>{
  if(pending||disposed||!options.current())return;
  pending=true;cancel.disabled=true;submit.disabled=true;hint.textContent='';dialog.setAttribute('aria-busy','true');
  controller=new AbortController();timer=setTimeout(()=>controller?.abort(),15000);
  try{const value=await options.request(controller.signal);if(disposed||!options.current())return;options.accepted(value);dialog.close()}
  catch(error){if(disposed||!options.current())return;const kind=options.classify(error);const refresh=kind==='changed'||kind==='expired';hint.textContent=labels[kind];reload.hidden=!refresh;submit.hidden=refresh;if(refresh)reload.focus();if(kind==='uncertain')submit.textContent=labels.retry}
  finally{if(timer)clearTimeout(timer);timer=undefined;pending=false;cancel.disabled=false;submit.disabled=false;dialog.setAttribute('aria-busy','false')}
 });
 dialog.addEventListener('cancel',event=>{if(pending)event.preventDefault()});
 dialog.addEventListener('close',dispose,{once:true});
 dialog.addEventListener('click',event=>{const box=dialog.getBoundingClientRect();if(!pending&&event.target===dialog&&(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom))dialog.close()});
 actions.append(cancel,reload,submit);dialog.append(title,description,hint,actions);options.root.append(dialog);dialog.showModal();cancel.focus();return dispose;
}
