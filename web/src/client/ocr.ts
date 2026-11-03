// Dialog-local OCR intent. Capability and admission authority remain on the server.
export type OCRCommand={enabled:false}|{enabled:true;provider_id:string;model_id:string;languages:string[];quality_preset?:string};
type Labels={model:string;languages:string;quality:string;chooseModel:string;chooseQuality:string;loading:string;unavailable:string;retry:string;external:string;presets:Record<string,string>};
type Model={processing_destination?:string;id:string;name:string;languages:string[];quality_presets:string[];default_quality_preset:string|null;available:boolean;outputs:string[]};
type Entry={provider:string;name:string;external:boolean;model:Model};
export function ocrControls(load:(signal:AbortSignal)=>Promise<any>,labels:Labels,locale:string,changed:()=>void){
 const element=document.createElement('section');element.className='ocr-settings';
 const toggleLabel=document.createElement('label');toggleLabel.className='ocr-toggle';
 const toggle=document.createElement('input');toggle.type='checkbox';toggle.id='ocr-enabled';toggle.setAttribute('role','switch');toggleLabel.append(document.createTextNode('OCR'),toggle);
 const fields=document.createElement('fieldset');fields.className='ocr-fields';
 const modelLabel=document.createElement('label');modelLabel.htmlFor='ocr-model';modelLabel.textContent=labels.model;
 const models=document.createElement('select');models.id='ocr-model';
 const languages=document.createElement('fieldset');languages.className='ocr-languages';const legend=document.createElement('legend');legend.textContent=labels.languages;languages.append(legend);
 const languageList=document.createElement('div');languages.append(languageList);
 const qualityLabel=document.createElement('label');qualityLabel.htmlFor='ocr-quality';qualityLabel.textContent=labels.quality;
 const quality=document.createElement('select');quality.id='ocr-quality';
 const notice=document.createElement('p');notice.className='ocr-notice';notice.id='ocr-processing-notice';models.setAttribute('aria-describedby',notice.id);notice.setAttribute('role','status');
 const state=document.createElement('p');state.className='ocr-state';state.setAttribute('role','status');
 const retry=document.createElement('button');retry.type='button';retry.textContent=labels.retry;
 fields.append(modelLabel,models,notice,languages,qualityLabel,quality);element.append(toggleLabel,fields,state,retry);
 let entries:Entry[]=[],ready=false,locked=false,disposed=false,controller:AbortController|null=null;
 let request=0;
 const placeholder=(select:HTMLSelectElement,label:string)=>{select.replaceChildren();const option=document.createElement('option');option.value='';option.textContent=label;select.append(option)};
 placeholder(models,labels.chooseModel);placeholder(quality,labels.chooseQuality);
 const selected=()=>models.value===''?undefined:entries[Number(models.value)];
 const sync=()=>{toggle.disabled=locked||(!ready&&!toggle.checked);fields.disabled=locked||!ready||!toggle.checked;quality.disabled=!(selected()?.model.quality_presets.length);retry.disabled=locked;changed()};
 function options(preferredLanguages:string[]=[],preset?:string){
  languageList.replaceChildren();placeholder(quality,labels.chooseQuality);
  const entry=selected();notice.textContent=entry?.external?labels.external.replace('{destination}',entry.model.processing_destination!):'';
  if(entry){
   const orderedLanguages=[...preferredLanguages.filter(language=>entry.model.languages.includes(language)),...entry.model.languages.filter(language=>!preferredLanguages.includes(language))];
   for(const language of orderedLanguages){const label=document.createElement('label');const input=document.createElement('input');input.type='checkbox';input.value=language;input.checked=preferredLanguages.includes(language);input.addEventListener('change',changed);let name=language;try{name=new Intl.DisplayNames([locale],{type:'language'}).of(language)||language}catch{}label.append(input,document.createTextNode(name));languageList.append(label)}
   for(const value of entry.model.quality_presets){const option=document.createElement('option');option.value=value;option.textContent=labels.presets[value]??value;quality.append(option)}
   const preference=preset??entry.model.default_quality_preset;if(preference!==null&&preference!==undefined&&entry.model.quality_presets.includes(preference))quality.value=preference;
  }
  sync();
 }
 models.addEventListener('change',()=>{const prior=Array.from(languageList.querySelectorAll<HTMLInputElement>('input:checked')).map(input=>input.value);options(prior)});
 quality.addEventListener('change',changed);toggle.addEventListener('change',sync);
 async function refresh(){
  const previous=selected();const previousLanguages=Array.from(languageList.querySelectorAll<HTMLInputElement>("input:checked")).map(input=>input.value);const previousPreset=quality.value;
  const version=++request;controller?.abort();controller=new AbortController();const current=controller;
  ready=false;state.textContent=labels.loading;retry.hidden=true;sync();
  const timeout=setTimeout(()=>current.abort(),15000);
  try{
   const capabilities=await load(current.signal);if(disposed||version!==request)return;
   if(!Array.isArray(capabilities?.ocr_plugins))throw new Error('Invalid capabilities');
   const next:Entry[]=[];
   for(const provider of capabilities.ocr_plugins){
    if(typeof provider.id!=='string'||typeof provider.name!=='string'||!Array.isArray(provider.models))throw new Error('Invalid provider');
    for(const model of provider.models){if(!model.available||!model.outputs?.includes('searchable_pdf'))continue;if(provider.external_processing===true&&(typeof model.processing_destination!=='string'||!model.processing_destination.trim()||model.processing_destination.length>256))throw new Error('Missing processing destination');if(typeof model.id!=='string'||typeof model.name!=='string'||!Array.isArray(model.languages)||!model.languages.length||!model.languages.every((v:unknown)=>typeof v==='string')||!Array.isArray(model.quality_presets)||!model.quality_presets.every((v:unknown)=>typeof v==='string')||(model.default_quality_preset!==null&&typeof model.default_quality_preset!=='string'))throw new Error('Invalid model');next.push({provider:provider.id,name:provider.name,external:provider.external_processing===true,model})}
   }
   entries=next;placeholder(models,labels.chooseModel);
   for(const [index,entry] of entries.entries()){const option=document.createElement('option');option.value=String(index);option.textContent=`${entry.name} · ${entry.model.name}`;models.append(option)}
   const preferred=capabilities.defaults?.ocr;const index=entries.findIndex(entry=>entry.provider===(previous?.provider??preferred?.provider_id)&&entry.model.id===(previous?.model.id??preferred?.model_id));
   if(index>=0)models.value=String(index);
   ready=entries.length>0;state.textContent=ready?'':labels.unavailable;retry.hidden=ready;
   options(index>=0?(previous?previousLanguages:Array.isArray(preferred?.languages)?preferred.languages:[]):[],index>=0?(previous?previousPreset:preferred?.quality_preset):undefined);
  }catch{if(!disposed&&version===request){state.textContent=labels.unavailable;retry.hidden=false;sync()}}
  finally{clearTimeout(timeout)}
 }
 retry.addEventListener('click',()=>void refresh());
 return {
  element,start:()=>void refresh(),lock:(value:boolean)=>{locked=value;sync()},
  valid:()=>!toggle.checked||(ready&&!!selected()&&languageList.querySelectorAll('input:checked').length>0&&languageList.querySelectorAll('input:checked').length<=32&&(!selected()!.model.quality_presets.length||quality.value!=='')),
  command:():OCRCommand=>{if(!toggle.checked)return {enabled:false};const entry=selected();if(!ready||!entry)throw new Error('OCR unavailable');const selectedLanguages=Array.from(languageList.querySelectorAll<HTMLInputElement>('input:checked')).map(input=>input.value);if(!selectedLanguages.length||selectedLanguages.length>32||entry.model.quality_presets.length&&!quality.value)throw new Error('OCR selection incomplete');return {enabled:true,provider_id:entry.provider,model_id:entry.model.id,languages:selectedLanguages,...(quality.value?{quality_preset:quality.value}:{})}},
  dispose:()=>{disposed=true;controller?.abort()}
 };
}
