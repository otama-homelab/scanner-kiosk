import { defineConfig } from 'astro/config';
import { readdirSync, readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';

// Keep PDF fonts, CMaps and codecs on this origin; previews never contact a CDN.
const pdfAssets={name:'pdf-preview-assets',generateBundle(){
 const sourceRoot=new URL('./vendor/liberation-fonts-1.07.4/',import.meta.url);
 const source=JSON.parse(readFileSync(new URL('SOURCE.json',sourceRoot),'utf8'));
 const digest=data=>createHash('sha256').update(data).digest('hex');
 if(digest(readFileSync(new URL(source.archive.file,sourceRoot)))!==source.archive.sha256)throw new Error('Liberation corresponding source mismatch');
 for(const [name,font] of Object.entries(source.runtime_fonts)){
  if(digest(readFileSync(new URL(`./node_modules/pdfjs-dist/standard_fonts/${name}`,import.meta.url)))!==font.sha256)throw new Error('Liberation runtime font mismatch');
 }
 const notices=[readFileSync(new URL('./node_modules/pdfjs-dist/LICENSE',import.meta.url),'utf8')];
 notices.push('Liberation Sans 1.07.4 corresponding source, licenses and build scripts are included in the project source distribution at web/vendor/liberation-fonts-1.07.4/.');
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
