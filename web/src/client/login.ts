export type LoginKey='username'|'password'|'signin'|'reset'|'failed'|'invalidCredentials'|'loginExpired'|'loginLimited'|'credentialsTooLong';
type Credentials={username:string;password:string};
type Options={
 root:HTMLElement;
 t:(key:LoginKey)=>string;
 current:()=>boolean;
 status:(key:LoginKey|null)=>void;
 classify:(error:unknown)=>{message:LoginKey;reset:boolean};
 submit:(credentials:Credentials,signal:AbortSignal)=>Promise<unknown>;
 reset:(signal:AbortSignal)=>Promise<unknown>;
 done:()=>Promise<void>;
};

// Passwords live only in the input and the explicit request. Leaving the view,
// completion, rejection or timeout clears the field and aborts pending work.
// An unknown response is never retried automatically with stored credentials.
export function passwordLogin(options:Options):()=>void{
 const form=document.createElement('form');form.className='auth-card';const title=document.createElement('h1');title.textContent=options.t('signin');form.append(title);
 const username=document.createElement('input');username.id='login-username';username.name='username';username.autocomplete='username';username.autocapitalize='off';username.spellcheck=false;username.required=true;username.maxLength=64;
 const password=document.createElement('input');password.id='login-password';password.name='password';password.type='password';password.autocomplete='current-password';password.required=true;password.maxLength=1024;
 for(const [input,key] of [[username,'username'],[password,'password']] as const){
  const label=document.createElement('label');label.htmlFor=input.id;label.textContent=options.t(key);input.setAttribute('aria-describedby','status');form.append(label,input);
 }
 const submit=document.createElement('button');submit.type='submit';submit.className='primary';submit.textContent=options.t('signin');
 const reset=document.createElement('button');reset.type='button';reset.textContent=options.t('reset');reset.hidden=true;
 form.append(submit,reset);options.root.append(form);
 let disposed=false,pending=false,controller:AbortController|null=null,timer:ReturnType<typeof setTimeout>|undefined;
 const active=()=>!disposed&&options.current();
 const run=async(resetting:boolean)=>{
  if(pending||!active())return;
  if(!resetting&&!form.reportValidity())return;
  if(!resetting&&(new TextEncoder().encode(password.value).length>1024||new TextEncoder().encode(username.value).length>256)){options.status('credentialsTooLong');return}
  pending=true;options.status(null);form.setAttribute('aria-busy','true');submit.disabled=reset.disabled=true;username.readOnly=password.readOnly=true;
  controller=new AbortController();const signal=controller.signal;timer=setTimeout(()=>controller?.abort(),15000);
  try{
   if(resetting){password.value='';await options.reset(signal)}else{await options.submit({username:username.value,password:password.value},signal)}
   if(active()&&!signal.aborted)await options.done();
  }catch(error){
   if(active()){
    const result=options.classify(error);options.status(result.message);reset.hidden=!result.reset;
    const invalid=result.message==='invalidCredentials';username.setAttribute('aria-invalid',String(invalid));password.setAttribute('aria-invalid',String(invalid));
   }
  }finally{
   password.value='';if(timer)clearTimeout(timer);timer=undefined;controller=null;pending=false;
   if(active()){form.removeAttribute('aria-busy');submit.disabled=reset.disabled=false;username.readOnly=password.readOnly=false}
  }
 };
 form.addEventListener('submit',event=>{event.preventDefault();void run(false)});
 reset.addEventListener('click',()=>void run(true));
 return ()=>{disposed=true;password.value='';controller?.abort();if(timer)clearTimeout(timer)};
}
