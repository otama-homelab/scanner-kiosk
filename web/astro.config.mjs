import { defineConfig } from 'astro/config';
import { readdirSync, readFileSync } from 'node:fs';

// Keep PDF fonts, CMaps and codecs on this origin; previews never contact a CDN.
const pdfAssets={name:'pdf-preview-assets',generateBundle(){
 const notices=[readFileSync(new URL('./node_modules/pdfjs-dist/LICENSE',import.meta.url),'utf8')];
 for(const folder of ['cmaps','standard_fonts','wasm']){
  const root=new URL(`./node_modules/pdfjs-dist/${folder}/`,import.meta.url);
  for(const file of readdirSync(root).sort()){
   if(file.startsWith('LICENSE')){notices.push(`${folder}/${file}\n${readFileSync(new URL(file,root),'utf8')}`);continue}
   if(file.startsWith('quickjs'))continue;
   this.emitFile({type:'asset',fileName:`pdfjs/${folder}/${file}`,source:readFileSync(new URL(file,root))});
  }
 }
 this.emitFile({type:'asset',fileName:'pdfjs/NOTICE.txt',source:notices.join('\n\n')});
}};
export default defineConfig({output:'static',cacheDir:'./.astro/cache',vite:{plugins:[pdfAssets],build:{assetsInlineLimit:0}}});
