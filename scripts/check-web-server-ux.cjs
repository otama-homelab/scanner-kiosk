const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {createHash}=require('node:crypto');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);

(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true,args:process.env.SCANNER_SERVER_UX_INSPECT_PORT?['--remote-debugging-port='+process.env.SCANNER_SERVER_UX_INSPECT_PORT]:[]});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(15000);
 const origin=process.env.SCANNER_TEST_ORIGIN,output=process.env.SCANNER_SERVER_UX_OUTPUT,checks=[],downloads=[],requests=[],errors=[],jobs=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/api/'))requests.push({method:r.method(),path:new URL(r.url()).pathname,key:r.headers()['idempotency-key']})});
 const api=(url,method='GET',data)=>page.evaluate(async({url,method,data})=>{const r=await fetch(url,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data)});if(!r.ok)throw new Error(url+' '+r.status);return r.json()},{url,method,data});
 const mode=value=>api('/__fixture/mode','POST',{Mode:value});
 const observe=()=>api('/__fixture/observe');
 const read=id=>api('/api/documents/'+id);
 const open=async id=>{await page.goto(origin+'/#/document/'+id);await page.locator('h1[data-document-title]').waitFor()};
 const primary=async()=>{
  const save=page.locator('.document-toolbar').getByRole('button',{name:'Save PDF',exact:true}).or(page.locator('.document-toolbar').getByRole('link',{name:'Save PDF',exact:true}));
  assert(await save.last().isEnabled(),'valid Save stays enabled');
  const primaries=page.locator('main .primary:visible, main .download-link:visible');assert.equal(await primaries.count(),1,'Save must remain the sole primary action with optional results');
  assert.match(await primaries.innerText(),/^Save PDF$/,'the sole primary action is Save');
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'Operate overflows viewport');
 };
 const download=async(name,id)=>{
  const event=page.waitForEvent('download');
  const toolbar=page.locator('.document-toolbar');const button=toolbar.getByRole('button',{name:'Save PDF',exact:true});
  if(await button.count())await button.click();else await toolbar.getByRole('link',{name:'Save PDF',exact:true}).click();
  const saved=await event;const file=path.join(output,name+'.pdf');await saved.saveAs(file);const bytes=fs.readFileSync(file);assert(bytes.subarray(0,5).equals(Buffer.from('%PDF-')));
  downloads.push({Name:name,DocumentID:id,SHA256:createHash('sha256').update(bytes).digest('hex')});
  await page.locator('.document-toolbar a.download-link').waitFor();return downloads.at(-1).SHA256;
 };
 const unchanged=async(id,before,hash)=>{const after=await read(id);assert.equal(after.revision,before.revision);assert.deepEqual(after.output,before.output);await primary();const r=await context.request.get(origin+'/api/documents/'+id+'/download');assert.equal(createHash('sha256').update(await r.body()).digest('hex'),hash)};
 const recognition=async()=>{await page.getByRole('button',{name:'Recognize text again',exact:true}).click();await page.getByRole('switch').check();const response=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname.endsWith('/reocr'));await page.getByRole('button',{name:'Start recognition',exact:true}).click();const result=await response;assert.equal(result.status(),202);const job=await result.json();jobs.push(job.id);await page.getByRole('dialog').waitFor({state:'detached'});return job.id};
 const terminal=async(id,state)=>{const deadline=Date.now()+15000;for(;;){const job=await api('/api/jobs/'+id);if(job.state===state)return;assert(!['failed','cancelled','succeeded','interrupted'].includes(job.state),'unexpected terminal state '+JSON.stringify(job));assert(Date.now()<deadline,'job did not reach '+state);await page.waitForTimeout(100)}};
 const candidate=async()=>{await page.getByRole('button',{name:'Adopt for whole document',exact:true}).waitFor();await primary()};
 try{
  await page.goto(origin);await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  const fixture=await api('/__fixture/bootstrap');
  const importFile=async(data,type)=>{
   await page.getByRole('button',{name:'Home',exact:true}).click();
   await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
   if(type==='image/jpeg'){await page.waitForFunction(()=>document.querySelector('input[data-photo-picker]')?.disabled===false);await page.locator('input[data-photo-picker]').setInputFiles({name:'synthetic.jpg',mimeType:type,buffer:Buffer.from(data,'base64')})}
   else await page.locator('.home-heading input[type=file]').setInputFiles({name:'synthetic.pdf',mimeType:type,buffer:Buffer.from(data,'base64')});
   await page.locator('h1[data-document-title]').waitFor();return page.locator('h1[data-document-title]').getAttribute('data-document-title');
  };
  await open(fixture.scan);
  for(const width of [390,1280]){await page.setViewportSize({width,height:900});await primary();assert.equal(await page.getByRole('dialog').count(),0);checks.push('shortest scan review to Save '+width)}
  await download('scan',fixture.scan);
  const pdf=await importFile(fixture.pdf,'application/pdf');await primary();await download('pdf',pdf);
  const photo=await importFile(fixture.photo,'image/jpeg');await primary();const photoHash=await download('photo',photo);let before=await read(photo);
  for(const width of [390,1280]){
   await page.setViewportSize({width,height:900});await primary();
   await page.getByRole('button',{name:'Crop photo',exact:true}).click();await page.keyboard.press('Escape');await page.getByRole('dialog').waitFor({state:'detached'});assert(await page.getByRole('button',{name:'Crop photo',exact:true}).evaluate(e=>document.activeElement===e));
   await page.getByRole('button',{name:'Recognize text again',exact:true}).click();await page.keyboard.press('Escape');await page.getByRole('dialog').waitFor({state:'detached'});
   await page.getByRole('button',{name:'Send to Paperless',exact:true}).click();await page.keyboard.press('Escape');await page.getByRole('dialog').waitFor({state:'detached'});
   await unchanged(photo,before,photoHash);checks.push('optional crop/reOCR/delivery cancellation and Save '+width);
  }
  assert.equal((await observe()).uploads,0,'no delivery without explicit confirmation');
  // Transport failures are injected only at the boundary; successful replies remain real.
  await page.route('**/correction/manual',r=>r.fulfill({status:503,json:{code:'unavailable'}}));
  await page.getByRole('button',{name:'Crop photo',exact:true}).click();await page.getByText('Adjust by pixels',{exact:true}).click();await page.getByLabel('Width',{exact:true}).fill('24');await page.getByRole('button',{name:'Preview crop',exact:true}).click();await page.getByRole('button',{name:'Apply crop',exact:true}).click();await page.getByRole('button',{name:'Cancel',exact:true}).click();await page.unroute('**/correction/manual');await unchanged(photo,before,photoHash);checks.push('failed optional crop preserves current export');
  await page.getByRole('button',{name:'Crop photo',exact:true}).click();await page.getByText('Adjust by pixels',{exact:true}).click();await page.getByLabel('Width',{exact:true}).fill('24');await page.getByRole('button',{name:'Preview crop',exact:true}).click();await page.getByRole('button',{name:'Apply crop',exact:true}).click();await page.getByRole('dialog').waitFor({state:'detached'});await primary();await download('corrected',photo);before=await read(photo);
  await recognition();await candidate();
  // Actual OCR source spans drive the optional filename suggestion.
  await page.locator('.ocr-spans button').first().click();await page.getByRole('button',{name:'Create filename suggestion',exact:true}).click();await page.locator('[data-structure-suggestion]').waitFor();await primary();
  const candidateOutput=await context.request.get(origin+'/api/documents/'+photo+'/download');const correctedHash=createHash('sha256').update(await candidateOutput.body()).digest('hex');
  await page.getByRole('button',{name:'Discard candidate',exact:true}).click();await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor({state:'detached'});await unchanged(photo,before,correctedHash);checks.push('real positioned reOCR candidate, generated suggestion and discard preserve Save');
  await recognition();await candidate();await page.getByRole('button',{name:'Adopt for whole document',exact:true}).click();await page.getByRole('button',{name:'Adopt for whole document',exact:true}).waitFor({state:'detached'});await primary();const adoptedHash=await download('adopted',photo);before=await read(photo);checks.push('explicit whole-document adoption uses actual corrected source');
  await mode(2);const cancelled=await recognition();await page.getByRole('button',{name:'Cancel recognition',exact:true}).waitFor();await primary();await page.getByRole('button',{name:'Cancel recognition',exact:true}).click();await terminal(cancelled,'cancelled');await page.reload();await page.locator('h1[data-document-title]').waitFor();await unchanged(photo,before,adoptedHash);checks.push('running optional reOCR cancellation retains enabled primary Save');
  await mode(1);const failed=await recognition();await terminal(failed,'failed');await page.reload();await page.locator('h1[data-document-title]').waitFor();await unchanged(photo,before,adoptedHash);await mode(0);checks.push('actual provider failure leaves adopted export unchanged');
  // A lost successful disposition is reconciled with GET; never resend its POST.
  await recognition();await candidate();let dispositions=0;
  await page.route('**/ocr-results/*/discard',async r=>{dispositions++;await r.fetch();await r.abort()});
  await page.getByRole('button',{name:'Discard candidate',exact:true}).click();await page.getByText('The result is uncertain. Check status before any new recognition request.',{exact:true}).waitFor();await page.getByRole('button',{name:'Check recognition status',exact:true}).click();await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor({state:'detached'});assert.equal(dispositions,1);await page.unroute('**/ocr-results/*/discard');await unchanged(photo,before,adoptedHash);checks.push('lost disposition response GET reconciliation POST1');
  await page.getByRole('button',{name:'Send to Paperless',exact:true}).click();await page.getByRole('button',{name:'Confirm send',exact:true}).click();await page.locator('[data-delivery-state="confirmed"]').waitFor();assert.equal((await observe()).uploads,1);await unchanged(photo,before,adoptedHash);checks.push('explicit delivery real upload checksum remote identity confirmed once');
  // Use the other saved documents for failed/unknown delivery ledgers.
  for(const [id,value,state] of [[fixture.scan,3,'failed'],[pdf,4,'unknown']]){
   await mode(value);await open(id);const current=await read(id);const r=await context.request.get(origin+'/api/documents/'+id+'/download');const hash=createHash('sha256').update(await r.body()).digest('hex');
   await page.getByRole('button',{name:'Send to Paperless',exact:true}).click();await page.getByRole('button',{name:'Confirm send',exact:true}).click();await page.locator('[data-delivery-state="'+state+'"]').waitFor();const uploads=(await observe()).uploads;await page.reload();await page.locator('[data-delivery-state="'+state+'"]').waitFor();assert.equal(await page.getByRole('button',{name:'Send to Paperless',exact:true}).count(),0);await unchanged(id,current,hash);assert.equal((await observe()).uploads,uploads);checks.push('real '+state+' delivery readonly recovery preserves Save and upload count');
  }
  await mode(0);await page.getByLabel('Exact Paperless task UUID').fill('12345678-1234-4234-8234-123456789abc');await page.getByRole('button',{name:'Reconcile task',exact:true}).click();await page.locator('[data-delivery-state="confirmed"]').waitFor();assert.equal((await observe()).uploads,3);checks.push('unknown exact UUID checksum GET reconciliation no resend');
  const counts=await observe();assert.equal(counts.documents,3);assert.equal(counts.deliveries,3);assert.equal(counts.reservations,0);assert.deepEqual(errors,[]);
  await open(photo);await page.waitForFunction(()=>{const image=document.querySelector('.preview-card img');return image?.complete&&image.naturalWidth>0});await page.locator('.ocr-spans button').first().waitFor();for(const width of [390,1280]){await page.setViewportSize({width,height:900});await primary();await page.screenshot({path:'/tmp/scanner49-server-ux-'+width+'.png',fullPage:true});checks.push('final sole primary Save '+width)}
  if(process.env.SCANNER_SERVER_UX_INSPECT_PORT){process.stdout.write('INSPECT_READY\n');await new Promise(resolve=>setTimeout(resolve,45000))}
  const receipt={passed:true,real_backend:true,checks,downloads,requests,counts,scope:'Current built Chromium; real SQLite/API/worker and native children; synthetic scan acquisition, OCR and Paperless loopback; no physical/live-service acceptance.'};fs.writeFileSync(path.join(output,'browser.json'),JSON.stringify(receipt,null,2)+'\n');process.stdout.write(JSON.stringify(receipt));
 }catch(e){console.error('FAILURE_STATE',await page.locator('body').innerText());console.error('ACTUAL_JOBS',JSON.stringify(await Promise.all(jobs.map(id=>api('/api/jobs/'+id)))));console.error('MUTATIONS',JSON.stringify(requests.filter(r=>r.method!=='GET')));throw e}finally{await context.close();await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
