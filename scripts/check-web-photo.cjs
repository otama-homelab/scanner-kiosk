const assert=require('node:assert/strict');
const fs=require('node:fs');
const {createHash}=require('node:crypto');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
const scenario=process.env.SCANNER_PHOTO_SCENARIO;
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(15000);
 const checks=[],requests=[],errors=[];page.on('pageerror',e=>errors.push(e.message));
 page.on('request',r=>{if(r.url().includes('/api/documents/import'))requests.push({path:new URL(r.url()).pathname,method:r.method(),key:r.headers()['idempotency-key'],sha:r.headers()['x-content-sha256']})});
 try{
  await page.goto(process.env.SCANNER_TEST_ORIGIN);await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  assert.equal(await page.getByRole('button',{name:'Camera',exact:true}).count(),1,'camera entry must exist independently of scanner readiness');
  const camera=page.locator('input[capture="environment"]');const picker=page.locator('input[data-photo-picker]');
  assert.equal(await camera.getAttribute('accept'),'image/jpeg,image/png');assert.equal(await picker.getAttribute('capture'),null);
  // Generate actual PNG/JPEG bytes through the browser encoder, with asymmetric corners.
  const images=await page.evaluate(()=>{const c=document.createElement('canvas');c.width=80;c.height=48;const x=c.getContext('2d');['#f01414','#14e614','#1414f0','#f0dc14'].forEach((v,i)=>{x.fillStyle=v;x.fillRect(i%2*40,Math.floor(i/2)*24,40,24)});return ['image/png','image/jpeg'].map(type=>({type,data:c.toDataURL(type,1).split(',')[1]}))});
  const upload=async(input,image)=>{await input.setInputFiles({name:'synthetic.'+(image.type==='image/png'?'png':'jpg'),mimeType:image.type,buffer:Buffer.from(image.data,'base64')})};
  const review=async()=>{await page.locator('h1[data-document-title]').waitFor();await page.getByRole('button',{name:'Save PDF',exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Scan next',exact:true}).count(),0);assert.equal(await page.locator('button.primary:visible').count(),1);const d=await page.evaluate(async()=>{const r=await fetch('/api/documents/'+location.hash.split('/').pop());return r.json()});assert.equal(d.purpose,'document');assert.equal(d.pages[0].source_type,'photo');assert.equal(d.pages[0].capture,null);return d};
  if(scenario==='reload'){
   let lost=false;await page.route('**/api/documents/import',async route=>{const response=await route.fetch();assert.equal(response.status(),202);lost=true;await route.abort('failed')});
   await upload(camera,images[1]);await page.getByRole('button',{name:'Check import request',exact:true}).waitFor();assert(lost);
   const identity=await page.evaluate(()=>JSON.parse(sessionStorage.getItem('scanner.photo.import')));assert.deepEqual(Object.keys(identity).sort(),['input_bytes','key','media_type','phase','sha256']);assert.equal(identity.sha256,createHash('sha256').update(Buffer.from(images[1].data,'base64')).digest('hex'));
   await page.reload();await page.getByRole('button',{name:'Check import request',exact:true}).waitFor();await page.getByRole('button',{name:'Check import request',exact:true}).click();await review();
   assert.equal(requests.filter(r=>r.path==='/api/documents/import').length,1);assert.equal(requests.filter(r=>r.path.endsWith('/reconcile')).length,1);assert.equal(requests[0].key,requests[1].key);
   const history=await page.evaluate(async()=>{const r=await fetch('/api/documents');return r.json()});assert.equal(history.items.length,1);assert.equal(await page.evaluate(()=>sessionStorage.getItem('scanner.photo.import')),null);checks.push('one upload, reload identity-only reconciliation, one document');
  }else if(scenario==='manual'){
   await upload(picker,images[0]);const doc=await review();
   assert.equal(await page.getByRole('button',{name:'Crop photo',exact:true}).count(),1,'manual normalized crop control must exist');
   await page.getByRole('button',{name:'Crop photo',exact:true}).click();await page.getByLabel('Left',{exact:true}).fill('8');await page.getByLabel('Width',{exact:true}).fill('64');await page.getByRole('button',{name:'Preview crop',exact:true}).click();
   const canvas=page.locator('.photo-crop canvas');assert.equal(await canvas.getAttribute('width'),'64');assert.deepEqual(await canvas.evaluate(c=>Array.from(c.getContext('2d').getImageData(5,5,1,1).data)),[240,20,20,255]);const original=await page.getByRole('link',{name:'View original upload',exact:true}).getAttribute('href');const retained=await context.request.get(new URL(original,process.env.SCANNER_TEST_ORIGIN).href);assert.equal(createHash('sha256').update(await retained.body()).digest('hex'),createHash('sha256').update(Buffer.from(images[0].data,'base64')).digest('hex'));await page.getByRole('button',{name:'Cancel',exact:true}).click();
   const read=()=>page.evaluate(async id=>(await fetch('/api/documents/'+id)).json(),doc.id);assert.equal((await read()).revision,doc.revision);
   await page.getByRole('button',{name:'Crop photo',exact:true}).click();await page.getByLabel('Left',{exact:true}).fill('8');await page.getByLabel('Width',{exact:true}).fill('64');await page.getByRole('dialog').getByRole('combobox',{name:'Rotate',exact:true}).selectOption('90');await page.getByRole('button',{name:'Preview crop',exact:true}).click();await page.getByRole('button',{name:'Apply crop',exact:true}).click();
   await page.locator('dialog.photo-crop').waitFor({state:'detached'});assert.equal((await read()).pages[0].rotation,90);await page.getByRole('button',{name:'Crop photo',exact:true}).click();await page.getByRole('button',{name:'Undo crop',exact:true}).click();await page.locator('dialog.photo-crop').waitFor({state:'detached'});
   assert.equal((await read()).pages[0].correction?.state,undefined);assert.equal((await read()).pages[0].rotation,90);checks.push('numeric crop actual preview pixels/cancel/apply/undo, separate page 90-degree rotation and immutable raw upload');
  }else{
   await camera.dispatchEvent('cancel');assert(await page.getByRole('button',{name:'Photos / Files',exact:true}).isEnabled());
   await upload(camera,images[1]);await review();
   // Save uses the real render admission/worker and browser download.
   const download=page.waitForEvent('download');await page.getByRole('button',{name:'Save PDF',exact:true}).click();const saved=await download;assert((await saved.suggestedFilename()).endsWith('.pdf'));
   await page.getByRole('button',{name:'Home',exact:true}).click();await upload(page.locator('input[data-photo-picker]'),images[0]);await review();checks.push('camera JPEG and independent picker PNG to real review, primary Save and actual PDF download');
  }
  assert.deepEqual(errors,[]);const receipt={passed:true,scenario,real_backend:true,checks,requests,scope:'Synthetic Chromium, real production serve/worker/SQLite and bounded photo child; no physical camera/screen reader/live service acceptance.'};if(process.env.SCANNER_PHOTO_RECEIPT)fs.writeFileSync(process.env.SCANNER_PHOTO_RECEIPT,JSON.stringify(receipt,null,2)+'\n');process.stdout.write(JSON.stringify(receipt));
 }finally{await context.close();await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
