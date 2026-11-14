import {iconButton} from './icons';
import {uiButton,uiButtonGroup} from './ui/button';
import type {Key} from './messages';

// Renderers own pixels, rotation and source identity; this view only sizes and scrolls them.
export function documentViewer(options:{content:HTMLElement|SVGSVGElement;title:string;t:(key:Key)=>string;onZoom?:(scale:number)=>void}){
 const {content,t}=options,element=document.createElement('div');element.className='document-viewer';
 const title=document.createElement('p');title.className='document-viewer-title';title.textContent=options.title;
 const viewport=document.createElement('div');viewport.className='document-viewer-viewport';viewport.tabIndex=0;viewport.setAttribute('role','region');viewport.setAttribute('aria-label',t('preview'));viewport.setAttribute('aria-description',t('viewerMoveHelp'));
 const stage=document.createElement('div'),surface=document.createElement('div');stage.className='document-viewer-stage';surface.className='document-viewer-surface';surface.append(content);stage.append(surface);viewport.append(stage);
 const controls=document.createElement('div');controls.className='document-viewer-controls';
 const command=(key:Key,run:()=>void,icon=false)=>{const button=uiButton(t(key),{variant:icon?'ghost':'outline',size:icon?'icon':'default'});button.addEventListener('click',run);return button};
 const fit=command('viewerFit',()=>setZoom(1)),out=iconButton(command('ocrZoomOut',()=>setZoom(zoom-.25),true),'ocrZoomOut'),into=iconButton(command('ocrZoomIn',()=>setZoom(zoom+.25),true),'ocrZoomIn');
 const level=document.createElement('output');level.setAttribute('aria-label',t('viewerZoom'));level.title=t('viewerZoom');controls.append(fit,uiButtonGroup(t('viewerZoom'),out,level,into));element.append(title,viewport,controls);
 let available=true,width=0,height=0,zoom=1,centerX=.5,centerY=.5,surfaceWidth=0,surfaceHeight=0,offsetX=0,offsetY=0;
 const controller=new AbortController(),signal=controller.signal;
 const remember=()=>{if(!surfaceWidth||!surfaceHeight||!viewport.clientWidth||!viewport.clientHeight)return;centerX=Math.max(0,Math.min(1,(viewport.scrollLeft+viewport.clientWidth/2-offsetX)/surfaceWidth));centerY=Math.max(0,Math.min(1,(viewport.scrollTop+viewport.clientHeight/2-offsetY)/surfaceHeight))};
 const layout=()=>{
  const w=viewport.clientWidth,h=viewport.clientHeight;if(!width||!height||!w||!h)return;
  const scale=Math.min(w/width,h/height)*zoom;surfaceWidth=width*scale;surfaceHeight=height*scale;offsetX=Math.max(0,(w-surfaceWidth)/2);offsetY=Math.max(0,(h-surfaceHeight)/2);
  stage.style.width=Math.max(w,surfaceWidth)+'px';stage.style.height=Math.max(h,surfaceHeight)+'px';surface.style.width=surfaceWidth+'px';surface.style.height=surfaceHeight+'px';
  viewport.scrollTo(offsetX+centerX*surfaceWidth-w/2,offsetY+centerY*surfaceHeight-h/2);
 };
 const setZoom=(value:number)=>{if(!available)return;remember();zoom=Math.max(1,Math.min(4,value));if(zoom===1)centerX=centerY=.5;level.value=Math.round(zoom*100)+'%';out.disabled=fit.disabled=zoom===1;into.disabled=zoom===4;viewport.dataset.zoomed=String(zoom>1);layout();options.onZoom?.(zoom)};
 setZoom(1);
 const observer=new ResizeObserver(layout);observer.observe(viewport);
 viewport.addEventListener('scroll',remember,{passive:true,signal});
 viewport.addEventListener('keydown',event=>{if(event.target!==viewport||event.ctrlKey||event.metaKey||event.altKey)return;if(['+','=','-','0'].includes(event.key)){event.preventDefault();setZoom(event.key==='0'?1:zoom+(event.key==='-'?-.25:.25))}},{signal});
 let drag:{id:number;x:number;y:number;left:number;top:number}|null=null;
 const release=()=>{drag=null;delete viewport.dataset.dragging};
 viewport.addEventListener('pointerdown',event=>{if(!available||event.pointerType!=='mouse'||event.button!==0||zoom===1)return;drag={id:event.pointerId,x:event.clientX,y:event.clientY,left:viewport.scrollLeft,top:viewport.scrollTop};viewport.setPointerCapture(event.pointerId)},{signal});
 viewport.addEventListener('pointermove',event=>{if(!drag||drag.id!==event.pointerId||Math.hypot(event.clientX-drag.x,event.clientY-drag.y)<3)return;event.preventDefault();viewport.dataset.dragging='true';viewport.scrollTo(drag.left+drag.x-event.clientX,drag.top+drag.y-event.clientY)},{signal});
 for(const name of ['pointerup','pointercancel','lostpointercapture'])viewport.addEventListener(name,release,{signal});
 content.addEventListener('dragstart',event=>event.preventDefault(),{signal});
 return {element,setSize(w:number,h:number){if(!Number.isFinite(w)||!Number.isFinite(h)||w<=0||h<=0)return;width=w;height=h;layout()},disable(){available=false;out.disabled=into.disabled=fit.disabled=true;viewport.dataset.zoomed='false'},dispose(){available=false;controller.abort();observer.disconnect();release()}};
}
