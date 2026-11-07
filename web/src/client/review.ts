/** Presentation only: the supplied cards retain server-derived order and edits. */
export type ReviewState={single:boolean;pageID:string|null;index:number};
export function reviewViews(grid:HTMLElement,labels:{overview:string;pageView:string;previous:string;next:string;page:string},state:ReviewState){
 const cards=Array.from(grid.children) as HTMLElement[];
 const root=document.createElement('div');root.className='review-pages';
 if(!cards.length){state.pageID=null;state.index=0;root.append(grid);return root}
 const modes=document.createElement('div');modes.className='review-view-switch';modes.setAttribute('role','group');modes.setAttribute('aria-label',labels.pageView);
 const make=(label:string,action:()=>void)=>{const control=document.createElement('button');control.type='button';control.textContent=label;control.addEventListener('click',action);return control};
 let selected=cards.findIndex(card=>card.dataset.pageId===state.pageID);if(selected<0)selected=Math.max(0,Math.min(state.index,cards.length-1));let single=state.single;
 const overview=make(labels.overview,()=>update(false));const detail=make(labels.pageView,()=>update(true));modes.append(overview,detail);
 const navigation=document.createElement('div');navigation.className='review-navigation';
 const previous=make(labels.previous,()=>{selected--;update(true)});const next=make(labels.next,()=>{selected++;update(true)});const position=document.createElement('span');position.setAttribute('role','status');previous.setAttribute('aria-label',labels.previous);previous.textContent='‹';next.setAttribute('aria-label',labels.next);next.textContent='›';navigation.append(previous,position,next);
 const thumbnails=document.createElement('div');thumbnails.className='review-thumbnails';thumbnails.setAttribute('role','group');thumbnails.setAttribute('aria-label',labels.overview);
 const choices:HTMLButtonElement[]=[];
 const source=(image:HTMLImageElement,full:boolean)=>{const url=full||image.dataset.thumbnailUnavailable==='true'?image.dataset.preview:image.dataset.thumbnail;if(url&&image.getAttribute('src')!==url)image.src=url};
 const fallback=(image:HTMLImageElement)=>image.addEventListener('error',()=>{if(image.dataset.thumbnail&&image.getAttribute('src')===image.dataset.thumbnail){image.dataset.thumbnailUnavailable='true';source(image,true)}});

 for(const [index,card] of cards.entries()){
  const image=card.querySelector('img');
  if(!image){const thumbnail=make('PDF '+(index+1),()=>{selected=index;update(true)});thumbnail.setAttribute('aria-label',card.dataset.pageLabel||labels.page+' '+(index+1));choices.push(thumbnail);thumbnails.append(thumbnail);const canvas=card.querySelector('canvas');if(canvas){const open=make('',()=>{selected=index;update(true);detail.focus()});open.className='preview-open';open.setAttribute('aria-label',labels.page+' '+(index+1));canvas.replaceWith(open);open.append(canvas)}continue}
  fallback(image);
  const open=make('',()=>{selected=index;update(true);detail.focus()});open.className='preview-open';open.setAttribute('aria-label',image.alt);image.replaceWith(open);open.append(image);
  const thumbnail=make('',()=>{selected=index;update(true)});thumbnail.setAttribute('aria-label',image.alt);const small=image.cloneNode(true) as HTMLImageElement;small.alt='';small.loading='lazy';fallback(small);source(small,false);thumbnail.append(small);choices.push(thumbnail);thumbnails.append(thumbnail)
 }
 function update(value:boolean){single=value;state.single=single;state.pageID=cards[selected].dataset.pageId??null;state.index=selected;grid.classList.toggle('single-page',single);overview.setAttribute('aria-pressed',String(!single));detail.setAttribute('aria-pressed',String(single));navigation.hidden=!single;thumbnails.hidden=!single;previous.disabled=selected===0;next.disabled=selected===cards.length-1;position.textContent=`${labels.page} ${selected+1} / ${cards.length}`;for(const [index,card] of cards.entries()){card.hidden=single&&index!==selected;const image=card.querySelector('img');if(image)source(image,single&&index===selected);choices[index].setAttribute('aria-pressed',String(index===selected))}}
 root.append(modes,navigation,thumbnails,grid);update(single);return root
}
