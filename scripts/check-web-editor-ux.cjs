const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
function fixturePDF(width=300,height=400){
 const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Count 1 /Kids [3 0 R] >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 '+width+' '+height+'] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>'];
 const stream='BT /F1 20 Tf 30 340 Td (Preview in place) Tj ET\n';objects.push('<< /Length '+Buffer.byteLength(stream)+' >>\nstream\n'+stream+'endstream','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>');
 let raw='%PDF-1.4\n';const offsets=[0];for(const [index,obj] of objects.entries()){offsets.push(Buffer.byteLength(raw));raw+=(index+1)+' 0 obj\n'+obj+'\nendobj\n'}const xref=Buffer.byteLength(raw);raw+='xref\n0 6\n0000000000 65535 f \n'+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n'+xref+'\n%%EOF\n';return Buffer.from(raw);
}
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(10000);
 const id='document_editor_0001',jobID='job_editor_0000001',errors=[],renders=[];
 let pdfDimensions=[300,400],pdfGeometryChecks=0;
 let state='queued',previewFailure=true,holdNextPreview=false,previewEntered,releasePreview;const previewBlocked=new Promise(resolve=>previewEntered=resolve);
 const doc={id,revision:2,purpose:'document',state:'review',created_at:'2026-11-07T03:00:00Z',expires_at:new Date(Date.now()+3600000).toISOString(),active_job_id:null,output:null,pages:[1,2].map(n=>({id:'page_editor_00000'+n,ordinal:n-1,rotation:0,source_type:'pdf'}))};
 const visitPDFPages=async()=>{const picker=page.locator('.document-pagebar select');for(const p of doc.pages){await picker.selectOption(p.id);await page.waitForFunction(id=>{const canvas=document.querySelector('[data-page-id="'+id+'"] canvas');return canvas&&!canvas.hidden&&canvas.width>0},p.id)}await picker.selectOption(doc.pages[0].id)};
 const reply=(r,json,status=200)=>r.fulfill({status,headers:{Date:new Date().toUTCString()},json});
 try{
  page.on('pageerror',e=>errors.push(e.message));await page.addInitScript(()=>document.addEventListener('securitypolicyviolation',e=>window.__csp=(window.__csp||[]).concat(e.violatedDirective)));
  await page.route('**/api/session',r=>reply(r,{authenticated:true,authenticate_type:'guest',principal_kind:'guest'}));
  await page.route('**/api/documents?*',r=>reply(r,{items:[{...doc,page_count:2}],next_cursor:null}));
  await page.route('**/api/documents/'+id,r=>reply(r,doc));
  await page.route('**/api/documents/'+id+'/pages/*/preview',async r=>{if(previewFailure){previewFailure=false;return r.fulfill({status:503,body:'Unavailable'})}if(holdNextPreview){holdNextPreview=false;previewEntered();await new Promise(resolve=>releasePreview=resolve);return r.fulfill({status:200,contentType:'application/pdf',body:fixturePDF(...pdfDimensions)}).catch(()=>{})}return r.fulfill({status:200,contentType:'application/pdf',headers:{'Content-Security-Policy':"default-src 'none'; frame-ancestors 'none'"},body:fixturePDF(...pdfDimensions)})});
  await page.route('**/api/documents/'+id+'/render',r=>{renders.push({body:r.request().postDataJSON(),headers:r.request().headers()});doc.active_job_id=jobID;return reply(r,{id:jobID,document_id:id,kind:'render',state:'queued'},202)});
  await page.route('**/api/jobs/'+jobID,r=>reply(r,{id:jobID,document_id:id,kind:'render',state}));
  await page.route('**/api/documents/'+id+'/download',r=>r.fulfill({status:200,contentType:'application/pdf',headers:{'Content-Disposition':'attachment; filename="scan.pdf"'},body:Buffer.from('%PDF-1.4\n% download fixture\n')}));
  await page.setViewportSize({width:390,height:844});await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);
  await page.locator('h1[data-document-title]').waitFor();
  await page.getByRole('button',{name:'Try again',exact:true}).click();await visitPDFPages();
  assert.equal(await page.locator('.preview-grid canvas').count(),2,'PDF pages preview inline in the overview');
  assert.equal(await page.locator('.page-more').count(),0);assert.equal(await page.getByRole('button',{name:'Replace with scan',exact:true}).count(),0);assert.equal(await page.getByRole('button',{name:'Insert scan before',exact:true}).count(),0);assert.equal(await page.locator('.page-actions button').count(),8,'four simple actions per page');const toolbarBox=await page.locator('.document-toolbar').boundingBox(),gridBox=await page.locator('.preview-grid').boundingBox();assert(gridBox.y>=toolbarBox.y+toolbarBox.height+20,'page blocks stay clear of the divider');
  assert.equal(await page.locator('.document-header button').count(),0,'document navigation uses the existing History header');
  const menu=page.locator('.document-more');await menu.locator('summary').click();assert(await menu.evaluate(e=>e.open));await menu.locator('.document-more-actions').click({position:{x:2,y:2}});assert(await menu.evaluate(e=>e.open),'inside menu clicks do not close it');await page.locator('h1[data-document-title]').click();assert(!(await menu.evaluate(e=>e.open)),'outside tap closes options');await menu.locator('summary').click();await page.keyboard.press('Escape');assert(!(await menu.evaluate(e=>e.open)));assert.equal(await page.evaluate(()=>document.activeElement.tagName),'SUMMARY');
  await menu.locator('summary').click();await page.getByRole('button',{name:'Output settings',exact:true}).click();await page.getByRole('dialog').waitFor();assert(!(await menu.evaluate(e=>e.open)),'selecting an option closes the menu');await page.keyboard.press('Escape');
  for(const dimensions of [[300,400],[900,200]])for(const viewport of [{width:320,height:844},{width:390,height:844},{width:1440,height:900},{width:1280,height:650}]){
   pdfDimensions=dimensions;await page.setViewportSize(viewport);await page.reload();await visitPDFPages();
   const viewer=page.locator('.preview-card:not([hidden]) .document-viewer');
   for(const percent of [100,125,400]){
    if(percent===125)await viewer.getByRole('button',{name:'Zoom in',exact:true}).click();
    if(percent===400)for(let i=0;i<11;i++)await viewer.getByRole('button',{name:'Zoom in',exact:true}).click();
    assert.equal(await viewer.locator('output').textContent(),percent+'%');
    const box=await viewer.evaluate(e=>{const c=e.querySelector('canvas').getBoundingClientRect(),s=e.querySelector('.document-viewer-surface').getBoundingClientRect();return {canvas:{width:c.width,height:c.height},surface:{width:s.width,height:s.height}}});
    for(const key of ['width','height'])assert(Math.abs(box.canvas[key]-box.surface[key])<=1,'PDF canvas follows fitted/zoomed surface '+JSON.stringify({dimensions,viewport,percent,box}));pdfGeometryChecks++;
   }
  }
  pdfDimensions=[300,400];await page.setViewportSize({width:390,height:844});await page.reload();await visitPDFPages();
  holdNextPreview=true;await page.getByRole('button',{name:'Save PDF',exact:true}).click();
  await previewBlocked;
  await page.getByText('Preparing download…',{exact:true}).waitFor();
  const gauge=page.getByRole('progressbar');await gauge.waitFor();assert.equal(await gauge.getAttribute('value'),null);assert.equal(await gauge.getAttribute('aria-labelledby'),'document-progress');
  assert.ok(page.url().endsWith('/document/'+id),'saving stays with the preview');assert(await page.getByRole('button',{name:'Save PDF',exact:true}).isDisabled());
  assert.equal(await page.locator('.preview-grid canvas').count(),2);
  assert.equal(await page.getByText('Queued',{exact:true}).count(),0);
  assert.equal(renders.length,1);assert.equal(renders[0].headers['if-match'],'"2"');assert.match(renders[0].headers['idempotency-key'],/^rq1_/);
  await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});document.dispatchEvent(new Event('visibilitychange'))});
  const download=page.waitForEvent('download');doc.active_job_id=null;doc.output={revision:doc.revision};doc.state='ready';state='succeeded';
  await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});document.dispatchEvent(new Event('visibilitychange'))});
  assert.equal((await download).suggestedFilename(),'scan.pdf','one save operation prepares and downloads');
  await page.getByRole('link',{name:'Save PDF',exact:true}).waitFor();assert.equal(await page.getByRole('progressbar').count(),0,'completed jobs remove the gauge');
  const second=page.waitForEvent('download');await page.getByRole('link',{name:'Save PDF',exact:true}).click();await second;assert.equal(renders.length,1,'fresh output downloads without another render');
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/history');await page.getByRole('heading',{name:'History',exact:true}).waitFor();
  releasePreview();
  await page.locator('.document-row canvas').waitFor().catch(async error=>{console.error(JSON.stringify({stage:'history thumbnail',url:page.url(),body:await page.locator('body').innerText(),previews:await page.locator('.pdf-page').evaluateAll(elements=>elements.map(e=>({status:e.textContent,canvasHidden:e.querySelector('canvas')?.hidden,bounds:e.getBoundingClientRect().toJSON()})))}));throw error});await page.waitForFunction(()=>{const c=document.querySelector('.document-row canvas');return c&&!c.hidden&&c.width>0});assert((await page.locator('.history-thumb').boundingBox()).height<=112);assert((await page.locator('.history-thumb canvas').boundingBox()).height<=112,'the complete PDF preview fits its thumbnail');assert.equal(await page.locator('.document-row .document-more').count(),0);assert((await page.locator('.history-title').innerText()).includes('2026'));await page.getByRole('button',{name:'Grid',exact:true}).click();assert.equal(await page.locator('.document-list').getAttribute('data-layout'),'grid');await page.reload();await page.getByRole('heading',{name:'History',exact:true}).waitFor();assert.equal(await page.locator('.document-list').getAttribute('data-layout'),'grid');await page.getByRole('button',{name:'List',exact:true}).click();assert.equal(await page.locator('.document-list').getAttribute('data-layout'),'list');
  await page.waitForFunction(()=>{const c=document.querySelector('.document-row canvas');return c&&!c.hidden&&c.width>0});
  doc.active_job_id=jobID;state='running';await page.reload();await page.getByRole('heading',{name:'History',exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'View processing status',exact:true}).count(),0);await page.getByRole('link',{name:'View processing status',exact:true}).waitFor();await page.getByRole('progressbar').waitFor();
  for(const layout of ['grid','list'])for(const width of [320,390,1440]){
   await page.setViewportSize({width,height:844});await page.getByRole('button',{name:layout==='grid'?'Grid':'List',exact:true}).click();
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'history does not overflow');
   if(process.env.SCANNER_TEST_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png',`-history-${layout}-${width}.png`),fullPage:true});
  }
  doc.active_job_id=null;state='succeeded';
  for(const locale of ['en','ja'])for(const theme of ['light','dark'])for(const width of [320,390,1440]){
   await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);await page.selectOption('#locale',locale);await page.locator('h1[data-document-title]').waitFor();await visitPDFPages();
   await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await page.setViewportSize({width,height:844});
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'no horizontal overflow');
   if(width<=600){assert.equal(await page.locator('.preview-card:visible').count(),1,'one full page is the stable focus');assert.equal(await page.locator('.document-pagebar select option').count(),2,'both PDF pages remain reachable');for(const button of await page.locator('.page-actions button:visible').all()){const box=await button.boundingBox();assert(box.width>=44&&box.height>=44,'page icons keep touch targets');}}
   if(process.env.SCANNER_TEST_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png',`-${locale}-${theme}-${width}.png`),fullPage:true});
  }
  await page.selectOption('#locale','en');
  let deleted=false;const deletes=[];
  await page.route('**/api/documents/'+id,route=>{if(route.request().method()!=='DELETE')return reply(route,doc);deletes.push(route.request().headers());deleted=true;return reply(route,{id:'job_cleanup_editor_01',document_id:id,kind:'cleanup',state:'queued'},202)});
  await page.route('**/api/documents?*',route=>reply(route,{items:deleted?[]:[{...doc,page_count:doc.pages.length}],next_cursor:null}));
  await page.route('**/api/documents/'+id+'/pages/*/thumbnail',route=>route.fulfill({status:200,contentType:'image/png',body:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aNFEAAAAASUVORK5CYII=','base64')}));
  for(const purpose of ['photo','document']){
   doc.purpose=purpose;doc.pages=[{...doc.pages[0],source_type:purpose==='photo'?'scan':'pdf'}];doc.active_job_id=null;doc.output=null;doc.state='review';deleted=false;
   await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);await page.locator('h1[data-document-title]').waitFor();
   if(purpose==='photo'){assert.equal(await page.locator('.photo-review').count(),1);assert.equal(await page.locator('.page-actions button').count(),2);assert.equal(await page.locator('.page-number').count(),0);assert.equal(await page.getByRole('button',{name:'Scan next page',exact:true}).count(),0)}
   if(process.env.SCANNER_TEST_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-last-'+purpose+'.png'),fullPage:true});
   const label=purpose==='photo'?'Delete photo':'Delete page';await page.locator('.page-actions').getByRole('button',{name:label,exact:true}).click();
   await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();assert.equal(deleted,false);
   await page.locator('.page-actions').getByRole('button',{name:label,exact:true}).click();await page.getByRole('dialog').getByRole('button',{name:label,exact:true}).click();
   await page.getByRole('heading',{name:'History',exact:true}).waitFor();assert.equal(await page.locator('.document-row').count(),0,'last image/page removes the document from History');
   assert.equal(deletes.at(-1)['if-match'],'"2"');assert.match(deletes.at(-1)['idempotency-key'],/^rq1_/);
  }
  assert.deepEqual(errors,[]);assert.deepEqual(await page.evaluate(()=>window.__csp||[]),[]);console.log(JSON.stringify({passed:true,pdf_geometry_checks:pdfGeometryChecks,checks:['inline PDF overview and history, explicit preview error recovery and cancellation of a blocked prior preview','always visible page action icons and persisted history grid/list','single save intent, revision and idempotency guards','preview remains while processing; visibility resume observes the same job without reposting','automatic download and fresh-output reuse','EN/JA light/dark 320/390/1440']}));
 }finally{await context.close();await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
