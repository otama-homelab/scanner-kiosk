import workerURL from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
import type {PDFDocumentLoadingTask,PDFWorker} from 'pdfjs-dist';

// One renderer at a time bounds page decoding on phones; finished tasks release their PDF data.
let queue=Promise.resolve();
let worker:PDFWorker|undefined;
export function pdfPreview(options:{url:string;label:string;rotation:number;loading:string;failed:string;retry:string;maxWidth?:number;current:()=>boolean}){
 const element=document.createElement('div');element.className='pdf-page';
 const canvas=document.createElement('canvas');canvas.setAttribute('role','img');canvas.setAttribute('aria-label',options.label);canvas.hidden=true;
 const status=document.createElement('p');status.setAttribute('role','status');status.textContent=options.loading;
 const retry=document.createElement('button');retry.type='button';retry.textContent=options.retry;retry.hidden=true;
 element.append(canvas,status,retry);
 let disposed=false,task:PDFDocumentLoadingTask|null=null,pending=false;
 const active=()=>!disposed&&options.current()&&element.isConnected;
 function start(){
  if(pending||!active())return;pending=true;retry.hidden=true;status.textContent=options.loading;
  queue=queue.then(async()=>{
   if(!active())return;
   let failed=false;
   try{
    const pdfjs=await import('pdfjs-dist/build/pdf.mjs');if(!active())return;
    pdfjs.GlobalWorkerOptions.workerSrc=workerURL;worker??=new pdfjs.PDFWorker({name:'scanner-page-preview'});
    task=pdfjs.getDocument({url:options.url,worker,withCredentials:true,isEvalSupported:false,disableFontFace:true,useSystemFonts:true,cMapUrl:'/pdfjs/cmaps/',cMapPacked:true,standardFontDataUrl:'/pdfjs/standard_fonts/',wasmUrl:'/pdfjs/wasm/'});
    const pdf=await task.promise;if(!active())return;
    const page=await pdf.getPage(1);const rotation=(page.rotate+options.rotation)%360;const original=page.getViewport({scale:1,rotation});
    const width=Math.min(options.maxWidth??1200,Math.max(320,element.clientWidth)*Math.min(2,devicePixelRatio||1));
    const viewport=page.getViewport({scale:Math.min(width/original.width,1400/original.height),rotation});
    canvas.width=Math.ceil(viewport.width);canvas.height=Math.ceil(viewport.height);
    await page.render({canvas,viewport}).promise;if(active()){canvas.hidden=false;status.textContent=''}
   }catch{failed=true;if(active()){status.textContent=options.failed;retry.hidden=false;retry.disabled=true}}
   finally{try{await task?.destroy()}finally{task=null;pending=false;if(failed){worker?.destroy();worker=undefined}retry.disabled=false}}
  }).catch(()=>{pending=false;if(active()){status.textContent=options.failed;retry.hidden=false}});
 }
 retry.addEventListener('click',start);
 const observer=new IntersectionObserver(entries=>{if(entries.some(e=>e.isIntersecting)){observer.disconnect();start()}},{rootMargin:'240px'});observer.observe(element);
 return {element,dispose(){disposed=true;observer.disconnect();void task?.destroy()}};
}
