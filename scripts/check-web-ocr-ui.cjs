const {reveal}=require('./web-controls.cjs');
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
if(process.env.SCANNER_TEST_SCENARIO==='reocr-native'){
 const {test}=require('node:test');
 test('TestReOCRNativeLifecycle',async()=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
  const context=await browser.newContext({ignoreHTTPSErrors:true,storageState:process.env.SCANNER_TEST_STORAGE});
  const page=await context.newPage();page.setDefaultTimeout(15000);
  const origin=process.env.SCANNER_TEST_ORIGIN,base='/api/documents/document_native_001';const checks=[];
  const get=async path=>{const r=await page.request.get(origin+path);assert.equal(r.status(),200);return r.json()};
  const control=async action=>assert.equal((await page.request.post(origin+'/__fixture/native-'+action)).status(),200);
  // The real API shares a 32-request burst across UI artifact loads and probes.
  // Refill between independent user flows rather than changing server limits.
  const settled=()=>page.waitForTimeout(4000);
  const download=async()=>{const r=await page.request.get(origin+base+'/download');assert.equal(r.status(),200);return require('node:crypto').createHash('sha256').update(await r.body()).digest('hex')};
  const wait=async(id,state)=>{let last;for(let i=0;i<60;i++){last=await get('/api/jobs/'+id);if(['succeeded','failed','cancelled'].includes(last.state)){assert.equal(last.state,state);return last}await page.waitForTimeout(1000)}throw Error('real job did not finish: '+JSON.stringify(last))};
  const start=async()=>{await page.getByRole('button',{name:'Recognize text again',exact:true}).click();await page.getByRole('switch').check();const accepted=page.waitForResponse(r=>new URL(r.url()).pathname===base+'/reocr'&&r.request().method()==='POST');await page.getByRole('button',{name:'Start recognition',exact:true}).click();const r=await accepted;assert.equal(r.status(),202);return (await r.json()).id};
  try{
   await settled();
   await page.goto(origin+'/#/document/document_native_001');await page.locator('h1[data-document-title]').waitFor();
   const before=await get(base),original=await download();
   const first=await start();await wait(first,'succeeded');await page.getByRole('button',{name:'Adopt for whole document',exact:true}).waitFor();assert.equal(await download(),original,'candidate never auto-publishes');
   await page.getByRole('button',{name:'Adopt for whole document',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-reocr-action]')?.disabled===false);
   const adopted=await get(base);assert.equal(adopted.revision,before.revision+1);const adoptedHash=await download();checks.push('real start candidate explicit whole-document adoption');await settled();
   const second=await start();await wait(second,'succeeded');await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor();assert.equal(await page.locator('.ocr-result-panel').count(),2);await page.locator('.ocr-spans button').first().click();assert(await page.locator('.ocr-highlight[visibility="visible"]').count()>0);assert.equal(await download(),adoptedHash);
   await page.getByRole('button',{name:'Discard candidate',exact:true}).click();await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor({state:'detached'});assert.equal(await download(),adoptedHash);checks.push('real same-page previous/candidate positions and discard retains output');await settled();
   await control('pause');const cancelled=await start();assert.equal(await download(),adoptedHash);await page.getByRole('button',{name:'Cancel recognition',exact:true}).click();await control('resume');await wait(cancelled,'cancelled');assert.equal(await download(),adoptedHash);checks.push('real queued cancellation retains usable download');await settled();
   await control('pause');const failed=await start();await control('corrupt');await control('resume');await wait(failed,'failed');await control('restore');await page.reload();await page.getByRole('button',{name:'Recognize text again',exact:true}).waitFor();assert.equal(await download(),adoptedHash);checks.push('real worker source-identity failure retains prior output');await settled();
   let lostJob=null,posts=0;await page.route('**'+base+'/reocr',async route=>{posts++;const response=await route.fetch();assert.equal(response.status(),202);lostJob=(await response.json()).id;await route.abort('failed')});
   await page.getByRole('button',{name:'Recognize text again',exact:true}).click();await page.getByRole('switch').check();await page.getByRole('button',{name:'Start recognition',exact:true}).click();await page.getByText('The result is uncertain. Check status before any new recognition request.',{exact:true}).waitFor();assert(lostJob);await wait(lostJob,'succeeded');await page.getByRole('button',{name:'Close',exact:true}).click();await page.getByRole('button',{name:'Check recognition status',exact:true}).click();await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor();assert.equal(posts,1);assert.equal(await download(),adoptedHash);await page.getByRole('button',{name:'Discard candidate',exact:true}).click();checks.push('real accepted command lost response reconciled without duplicate POST');
   const after=await get(base);assert.equal(after.created_at,before.created_at);assert.equal(after.expires_at,before.expires_at);assert.equal(after.revision,adopted.revision);
   require('node:fs').writeFileSync(process.env.SCANNER_TEST_RECEIPT,JSON.stringify({passed:true,scenario:'reocr-native',checks,first_job:first,cancelled_job:cancelled,failed_job:failed,unknown_job:lostJob,retained_output_sha256:adoptedHash}));
  }catch(error){console.error('native lifecycle DOM:',await page.locator('body').innerText());throw error}finally{await control('resume');await context.close();await browser.close()}
 });
}else if(process.env.SCANNER_TEST_SCENARIO){
 const {test}=require('node:test');
 const scenario=process.env.SCANNER_TEST_SCENARIO;
 test(scenario==='structure-suggestion'?'TestStructureFilenameSuggestion':scenario==='reocr-start'?'TestReOCRStartLifecycle':scenario==='reocr-adopt'?'TestReOCRWholeDocumentAdoption':'TestReOCRComparePositions',async(t)=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
  const checks=[],requests=[];
  try{for(const width of scenario==='reocr-compare'?[1280,390,1440,320]:[1280,390]){
   const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width,height:844}});const page=await context.newPage();page.setDefaultTimeout(2500);
   const before=requests.filter(r=>r.path.endsWith('/reocr')).length;const id='document_reocr_001',jobid='job_reocr_0000001',old='result_reocr_old01',candidate='result_reocr_new01',pageid='page_reocr_000001';let active=null,cancelled=false,disposition='candidate',lose=false,stale=false,loseStart=false,offline=false;
   const summary=(rid,state)=>({id:rid,state,input_revision:1,url:'/api/documents/'+id+'/ocr-results/'+rid});
   const result=rid=>({...summary(rid,rid===old?'adopted':'candidate'),settings:{ocr:{provider_id:'remote',model_id:'model',model_version:'fixture-v1'}},pages:[{page_id:pageid,source_type:rid===old?'scan':'pdf',rotation:90,pixel_width:200,pixel_height:100,original_sha256:'a'.repeat(64),effective_sha256:(rid===old?'a':'b').repeat(64),pdf_text_origin:rid===old?'recognition':'retained-source',...(rid===old?{}:{pdf_raster:{DPI:300,SourceRotation:90}}),artifacts:Object.fromEntries(['effective','preview','text','positions'].map(kind=>[kind,'/api/documents/'+id+'/ocr-results/'+rid+'/pages/'+pageid+'/'+kind]))}]});
   const reply=(r,json,status=200)=>r.fulfill({status,headers:{Date:new Date().toUTCString()},json});
   await page.route('**/api/**',async r=>{const path=new URL(r.request().url()).pathname;requests.push({path,method:r.request().method(),body:r.request().postDataJSON(),key:r.request().headers()['idempotency-key'],match:r.request().headers()['if-match']});
    if(path==='/api/session')return reply(r,{authenticated:true,principal_kind:'guest',authenticate_type:'guest'});
    if(path==='/api/capabilities'&&offline)return reply(r,{code:'provider_unavailable'},503);
    if(path==='/api/capabilities')return reply(r,{ocr_plugins:[{id:'remote',name:'Remote',external_processing:true,models:[{id:'model',name:'OCR model',available:true,outputs:['searchable_pdf'],languages:['ja'],quality_presets:['standard'],default_quality_preset:'standard',processing_destination:'Family OCR server'}]}],defaults:{ocr:{provider_id:'remote',model_id:'model',languages:['ja'],quality_preset:'standard'}}});
    if(path==='/api/documents/'+id)return reply(r,{id,revision:disposition==='adopted'||stale?2:1,purpose:'document',state:'ready',created_at:'2026-11-07T03:00:00Z',expires_at:'2099-01-01T00:00:00Z',active_job_id:active,output:{revision:disposition==='adopted'?2:1},ocr_results:scenario==='reocr-start'?[]:disposition==='candidate'?[summary(old,'adopted'),summary(candidate,'candidate')]:[summary(disposition==='adopted'?candidate:old,'adopted')],pages:[{id:pageid,rotation:0,capture:{size:'a4',color:'gray',dpi:300}},...(scenario==='reocr-adopt'?[{id:'page_reocr_000002',rotation:90,capture:{size:'a4',color:'gray',dpi:300}}]:[])]});
    if(path.endsWith('/download'))return r.fulfill({status:200,headers:{'Content-Type':'application/pdf','Content-Disposition':'attachment; filename="scan.pdf"'},body:Buffer.from('%PDF-1.4\nunchanged browser fixture\n%%EOF')});
    if(path.endsWith('/adopt')||path.endsWith('/discard')){if(stale)return reply(r,{code:'source_changed'},412);disposition=path.endsWith('/adopt')?'adopted':'discarded';await new Promise(resolve=>setTimeout(resolve,50));return lose?r.abort('failed'):reply(r,{id,revision:disposition==='adopted'?2:1})}
    if(path.endsWith('/ocr-results/'+old)||path.endsWith('/ocr-results/'+candidate)){const detail={...result(path.endsWith(old)?old:candidate),state:path.endsWith(candidate)?disposition:'adopted'};if(scenario==='reocr-adopt')detail.pages.push({...detail.pages[0],page_id:'page_reocr_000002',transcript_unavailable:true,artifacts:{}});return reply(r,detail)}
    if(path.includes('/ocr-results/')&&path.endsWith('/positions'))return reply(r,{page_id:pageid,width:200,height:100,spans:[{text:'<script>window.ocrInjected=true</script>',box:{x:20,y:10,width:40,height:20}},{text:'日本語 second',box:{x:100,y:50,width:60,height:10}}]});
    if(path.includes('/ocr-results/')&&path.endsWith('/text'))return r.fulfill({status:200,contentType:'text/plain',body:'<script>window.ocrInjected=true</script>\n日本語 second'});
    if(path.endsWith('/preview')||path.endsWith('/effective')||path.endsWith('/thumbnail'))return r.fulfill({status:200,contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><rect width="200" height="100" fill="white"/><path d="M20 10h40v20H20z" fill="black"/></svg>'});
    if(path.endsWith('/reocr')){active=jobid;return loseStart?r.abort('failed'):reply(r,{id:jobid,document_id:id,kind:'render',state:'queued',input_revision:1},202)}
    if(path.endsWith('/cancel')){cancelled=true;active=null;return reply(r,{id:jobid,document_id:id,kind:'render',state:'cancelled',input_revision:1})}
    if(path==='/api/jobs/'+jobid)return reply(r,{id:jobid,document_id:id,kind:'render',state:cancelled?'cancelled':'queued',input_revision:1});
    return r.fulfill({status:404,json:{}});
   });
   page.on('console',e=>{if(e.type()==='error')console.error(e.text())});page.on('pageerror',e=>console.error(e.message));await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);try{await page.locator('h1[data-document-title]').waitFor()}catch(e){console.error(await page.locator('body').innerText(),requests);throw e}
   assert.equal(requests.filter(r=>r.path.endsWith('/reocr')).length,before);
   if(scenario==='structure-suggestion'){
    const panel=page.locator('.ocr-result-panel').first(),generated=panel.locator('[data-structure-suggestion]'),create=panel.getByRole('button',{name:'Create filename suggestion',exact:true});
    await panel.locator('.ocr-spans button').first().waitFor();assert.equal(await create.count(),0,'no suggestion control until an actual source span is selected');assert.equal(await generated.count(),0,'viewing never generates');
    const save=page.locator('.document-toolbar a.download-link'),href=await save.getAttribute('href');
    const download=async()=>{const event=page.waitForEvent('download');await save.click();const file=await event;assert.equal(file.suggestedFilename(),'scan.pdf');return require('node:fs').readFileSync(await file.path()).toString('base64')};
    const original=await download();const span=panel.locator('.ocr-spans button').first();await span.click();assert.equal(await create.count(),1,'selected authorized span must offer explicit filename suggestion');await create.click();await generated.waitFor();assert.match(await generated.innerText(),/Generated filename suggestion/);assert.match(await generated.innerText(),/not a transcript/);assert.match(await generated.innerText(),new RegExp(old));assert.match(await generated.innerText(),/Source revision 1/);assert.match(await generated.innerText(),new RegExp(pageid));assert.match(await generated.innerText(),new RegExp('a'.repeat(64)));assert.match(await generated.innerText(),/Item 1/);
    assert.equal(await generated.locator('script').count(),0);assert.equal(await page.evaluate(()=>window.ocrInjected),undefined);
    const source=generated.getByRole('button',{name:'Show source',exact:true});await source.focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>document.activeElement?.matches('.ocr-spans button'));assert.equal(await span.getAttribute('aria-pressed'),'true');assert.equal(await panel.locator('.ocr-highlight').getAttribute('x'),'20');assert.equal(await panel.locator('.ocr-highlight').getAttribute('visibility'),'visible');
    assert.equal(await save.getAttribute('href'),href);assert.equal(await download(),original);assert.equal(requests.filter(r=>r.method!=='GET').length,0,'suggestions never mutate, adopt, rename or render');
    await page.locator('#locale').selectOption('ja');await page.locator('.ocr-spans button').first().waitFor();assert.equal(await page.locator('[data-structure-suggestion]').count(),0);await page.locator('#locale').selectOption('en');await page.locator('.ocr-spans button').first().waitFor();assert.equal(await page.locator('[data-structure-suggestion]').count(),0);
    assert(requests.some(r=>r.path.endsWith('/positions'))&&requests.some(r=>r.path.endsWith('/text')),'source comes from exact result artifact requests');checks.push('TestStructureFilenameSuggestion '+width);await context.close();continue;
   }
   if(scenario==='reocr-adopt'){
    const adopt=page.getByRole('button',{name:'Adopt for whole document',exact:true});assert.equal(await adopt.count(),1,'candidate requires explicit whole-document adoption');
    assert.equal(requests.filter(r=>r.path.endsWith('/adopt')).length,width===1280?0:2,'opening never adopts');
    await adopt.click();await page.waitForFunction(()=>document.querySelector('[data-reocr-action]')===document.activeElement);assert.equal(disposition,'adopted');assert.equal(await adopt.count(),0);assert(await page.locator('a.download-link').isVisible());
    const command=requests.filter(r=>r.path.endsWith('/adopt')).at(-1);assert.deepEqual(command.body,{});assert.equal(command.match,'"1"');assert.match(command.key,/^rq1_/);
    await t.test('TestReOCRDiscardRecovery '+width,async()=>{
     disposition='candidate';lose=true;await page.reload();await page.getByRole('button',{name:'Discard candidate',exact:true}).click();await page.getByRole('button',{name:'Check recognition status',exact:true}).click();await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor({state:'detached'});assert.equal(disposition,'discarded');assert(await page.locator('a.download-link').isVisible());assert.equal(requests.filter(r=>r.path.endsWith('/discard')).length,width===1280?1:2,'lost disposition response must not duplicate command');
     disposition='candidate';lose=false;await page.reload();await page.getByRole('button',{name:'Adopt for whole document',exact:true}).waitFor();stale=true;await page.getByRole('button',{name:'Adopt for whole document',exact:true}).click();await page.getByText('This document changed. Reload it before editing.',{exact:true}).waitFor();assert(await page.locator('a.download-link').isVisible());stale=false;
    });
    await t.test('TestReOCRKeyboardFocus '+width,async()=>{
     disposition='adopted';await page.reload();const action=page.getByRole('button',{name:'Recognize text again',exact:true});await action.click();await page.keyboard.press('Escape');assert(await action.evaluate(e=>document.activeElement===e));await action.click();await page.getByRole('button',{name:'Close',exact:true}).click();assert(await action.evaluate(e=>document.activeElement===e));
     const pages=page.locator('.ocr-results>label select');await pages.selectOption('page_reocr_000002');await page.getByText('No transcript is available for this result page.',{exact:true}).waitFor();assert.equal(await page.locator('.ocr-spans button').count(),0);assert.equal(await page.locator('.ocr-highlight[visibility="visible"]').count(),0);await page.reload();assert.equal(await pages.inputValue(),'page_reocr_000002');await pages.selectOption(pageid);await page.locator('.ocr-spans button').first().waitFor();assert.equal(await page.locator('.ocr-highlight[visibility="visible"]').count(),0);
     await page.locator('#locale').selectOption('ja');await page.getByRole('button',{name:'文字をもう一度認識',exact:true}).waitFor();await page.locator('#locale').selectOption('en');await action.waitFor();assert.equal(requests.filter(r=>r.path.endsWith('/reocr')).length,before,'locale changes never recognize');
     loseStart=true;await action.click();await page.getByRole('switch').check();await page.getByRole('button',{name:'Start recognition',exact:true}).click();await page.getByText('The result is uncertain. Check status before any new recognition request.',{exact:true}).waitFor();await page.getByRole('button',{name:'Close',exact:true}).click();await page.reload();await page.getByRole('button',{name:'Cancel recognition',exact:true}).waitFor();assert.equal(requests.filter(r=>r.path.endsWith('/reocr')).length,before+1,'reload checks existing job without resending uncertain start');assert(await page.locator('a.download-link').isVisible());
    });checks.push('TestReOCRWholeDocumentAdoption '+width,'TestReOCRDiscardRecovery '+width,'TestReOCRKeyboardFocus '+width);await context.close();continue;
   }
   if(scenario==='reocr-compare'){
    const panels=page.locator('.ocr-result-panel');assert.equal(await panels.count(),2,'same-page comparison must render old and candidate');
    await panels.first().getByRole('button',{name:'1. <script>window.ocrInjected=true</script>',exact:true}).click();
    const rect=panels.first().locator('rect.ocr-highlight');assert.equal(await rect.getAttribute('x'),'20');assert.equal(await rect.getAttribute('width'),'40');
    const geometry=await rect.evaluate(el=>{const m=el.parentElement.getScreenCTM(),p=new DOMPoint(20,10).matrixTransform(m),q=new DOMPoint(60,30).matrixTransform(m),r=el.getBoundingClientRect();return {x:r.x,y:r.y,w:r.width,h:r.height,expected:[Math.min(p.x,q.x),Math.min(p.y,q.y),Math.abs(p.x-q.x),Math.abs(p.y-q.y)]}});for(const [i,k] of ['x','y','w','h'].entries())assert(Math.abs(geometry[k]-geometry.expected[i])<1,'rendered highlight must follow rotated source coordinates');
    assert.equal(await panels.first().locator('svg').getAttribute('viewBox'),'0 0 100 200');assert.equal(await panels.last().locator('svg').getAttribute('viewBox'),'0 0 200 100');
    assert((await panels.first().locator('image').getAttribute('href')).includes(old));assert((await panels.last().locator('image').getAttribute('href')).includes(candidate));
    assert.equal(await page.evaluate(()=>window.ocrInjected),undefined);
    await t.test('TestReOCRReadingOrder '+width,async()=>{for(const panel of [panels.first(),panels.last()])assert.deepEqual(await panel.locator('.ocr-spans button').allTextContents(),['1. <script>window.ocrInjected=true</script>','2. 日本語 second'])});
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'comparison must fit viewport');
    await page.evaluate(()=>{document.activeElement.blur();scrollTo(0,0)});await page.screenshot({path:'/tmp/scanner-kiosk-reocr-compare-'+width+'.png',fullPage:true});
    await page.route('**/ocr-results/**/positions',r=>reply(r,{page_id:'foreign_page',width:200,height:100,spans:[]}));
    await page.route('**/ocr-results/**/effective',r=>r.fulfill({status:404}));
    await page.reload();await page.getByText('Text positions are unavailable. No highlights or reading order are inferred.',{exact:true}).first().waitFor();assert.equal(await page.locator('.ocr-spans button').count(),0);assert.equal(await page.locator('.ocr-highlight[visibility="visible"]').count(),0);await page.getByText('The matching source preview is unavailable.',{exact:true}).waitFor();assert.equal(await page.evaluate(()=>window.ocrInjected),undefined);
    checks.push('TestReOCRComparePositions '+width,'TestReOCRReadingOrder '+width,'Missing preview and foreign positions fail honestly '+width);await context.close();continue;
   }
   const action=page.getByRole('button',{name:'Recognize text again',exact:true});assert.equal(await action.count(),1,'document must offer explicit optional reOCR');
   await action.click();await page.getByRole('switch').check();await page.getByText('Images will be sent to Family OCR server for OCR.',{exact:true}).waitFor();
   await page.getByRole('button',{name:'Start recognition',exact:true}).click();await page.getByRole('button',{name:'Cancel recognition',exact:true}).waitFor();
   assert(await page.locator('a.download-link').isVisible(),'old download retained during reOCR');
   const command=requests.find(r=>r.path.endsWith('/reocr'));assert.deepEqual(command.body,{format:'pdf',ocr:{enabled:true,provider_id:'remote',model_id:'model',languages:['ja'],quality_preset:'standard'}});
   await page.getByRole('button',{name:'Cancel recognition',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#main')?.getAttribute('aria-busy')==='false'&&document.querySelector('[data-reocr-action]')?.disabled===false);assert(await page.locator('a.download-link').isVisible());
   offline=true;await page.reload();await page.getByRole('button',{name:'Recognize text again',exact:true}).click();await page.getByText('Text recognition is unavailable. You can still choose a standard PDF.',{exact:true}).waitFor();assert(await page.getByRole('button',{name:'Start recognition',exact:true}).isDisabled());assert(await page.locator('a.download-link').isVisible());
   checks.push('TestReOCRStartLifecycle '+width);await context.close();
  }}finally{await browser.close()}
  require('node:fs').writeFileSync(process.env.SCANNER_TEST_RECEIPT,JSON.stringify({passed:true,scenario:process.env.SCANNER_TEST_SCENARIO,checks,requests}));
 });
}else{
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:390,height:844}});const page=await context.newPage();page.setDefaultTimeout(10000);
 const errors=[],posts=[],checks=[];page.on('pageerror',error=>errors.push(error.message));
 const doc='document_ocr_ui001';let mode='none',capabilityReads=0,active=null;
 const model={id:'native-model',name:'Japanese/English',available:true,outputs:['searchable_pdf'],languages:['ja','en'],quality_presets:['standard','accurate'],default_quality_preset:'standard'};
 const metadata=()=>({ocr_plugins:mode==='none'?[]:[{id:mode==='local'?'tesseract':'llm',name:mode==='local'?'Tesseract':'Configured OCR',external_processing:mode!=='local',models:[{...model,...(mode==='local'||mode==='invalid'?{}:{processing_destination:'Family OCR server'})}]}],defaults:{ocr:{provider_id:mode==='local'?'tesseract':'llm',model_id:model.id,languages:['ja','en'],quality_preset:'standard'}}});
 const reply=(route,json,status=200)=>route.fulfill({status,headers:{Date:new Date().toUTCString()},json});
 try{
  await page.route('**/api/session',r=>reply(r,{authenticated:true,principal_kind:'guest',authenticate_type:'guest'}));
  await page.route('**/api/capabilities',r=>{capabilityReads++;return reply(r,mode==='offline'?{code:'provider_unavailable'}:metadata(),mode==='offline'?503:200)});
  await page.route('**/api/documents/'+doc,r=>reply(r,{id:doc,active_job_id:active,revision:1,purpose:'document',state:'review',created_at:'2026-11-07T03:00:00Z',expires_at:'2099-01-01T00:00:00Z',pages:[{id:'page_ocr_ui000001',ordinal:0,rotation:0,capture:{size:'a4',color:'gray',dpi:300}}]}));
  await page.route('**/pages/*/preview',r=>r.fulfill({status:200,contentType:'image/png',body:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6l2kAAAAASUVORK5CYII=','base64')}));
  const job={id:'job_ocr_ui0000001',document_id:doc,kind:'render',state:'queued',input_revision:1,error_code:null};
  await page.route('**/api/documents/'+doc+'/render',r=>{posts.push(r.request().postDataJSON());active=job.id;return reply(r,job,202)});
  await page.route('**/api/jobs/'+job.id,r=>reply(r,job));
  const open=async()=>{await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+doc);await page.locator('h1[data-document-title]').waitFor();const loaded=page.waitForResponse('**/api/capabilities');await (await reveal(page.getByRole('button',{name:'Output settings',exact:true,includeHidden:true}))).click();await loaded;};
  await open();
  await page.waitForFunction(()=>document.querySelector('.ocr-settings')?.hidden===true);
  assert.equal(await page.getByRole('switch').count(),0,'unconfigured OCR must not be offered');
  assert(await page.getByRole('dialog').getByRole('button',{name:'Save PDF',exact:true}).isEnabled());if(process.env.SCANNER_TEST_SCREENSHOTS)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOTS+'/ocr-none.png',fullPage:true});checks.push('No configured OCR hides its controls and keeps ordinary PDF saving');
  await page.keyboard.press('Escape');mode='local';await open();await page.getByRole('switch').waitFor();
  assert(!(await page.getByRole('switch').isChecked()));assert(!(await page.locator('.ocr-fields').isVisible()));
  if(process.env.SCANNER_TEST_SCREENSHOTS)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOTS+'/ocr-off.png',fullPage:true});const compact=await page.getByRole('dialog').boundingBox();await page.getByRole('switch').check();assert(await page.locator('.ocr-fields').isVisible());
  const expanded=await page.getByRole('dialog').boundingBox();assert(expanded.height>compact.height);assert.equal(expanded.width,compact.width);
  assert.equal(await page.locator('.ocr-languages input:checked').count(),2);assert.equal(await page.locator('#ocr-model').inputValue(),'0');
  await page.getByRole('switch').uncheck();assert(!(await page.locator('.ocr-fields').isVisible()));checks.push('URL-free local plugin is offered; details appear only when searchable PDF is selected');
  await page.keyboard.press('Escape');mode='remote';await open();await page.getByRole('switch').waitFor();await page.getByRole('switch').check();await page.getByText('Images will be sent to Family OCR server for OCR.',{exact:true}).waitFor();
  assert.equal(posts.length,0,'enabling OCR settings never sends document content');if(process.env.SCANNER_TEST_SCREENSHOTS)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOTS+'/ocr-remote.png',fullPage:true});checks.push('Configured remote plugin discloses its destination before explicit PDF creation');
  await page.keyboard.press('Escape');mode='offline';await open();await page.locator('.ocr-state').filter({hasText:'Text recognition is unavailable'}).waitFor();
  assert(await page.getByRole('dialog').getByRole('button',{name:'Save PDF',exact:true}).isEnabled());
  mode='local';await page.getByRole('dialog').getByRole('button',{name:'Try again',exact:true}).click();await page.getByRole('switch').waitFor();checks.push('OCR metadata failure preserves plain PDF and explicit retry recovers configured plugin');
  await page.keyboard.press('Escape');mode='invalid';await open();await page.locator('.ocr-state').filter({hasText:'Text recognition is unavailable'}).waitFor();assert(await page.getByRole('switch').isDisabled());
  assert(await page.getByRole('dialog').getByRole('button',{name:'Save PDF',exact:true}).isEnabled());await page.keyboard.press('Escape');
  mode='offline';const before=capabilityReads;await page.getByRole('button',{name:'Save PDF',exact:true}).click();await page.getByText('Preparing download…',{exact:true}).waitFor();
  assert.equal(capabilityReads,before);assert.deepEqual(posts,[{format:'pdf',pdf_encoding:'jpeg',ocr:{enabled:false}}]);checks.push('Missing destination fails closed for OCR; direct plain PDF never depends on OCR discovery');
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:true,checks}));
 }finally{await context.close();await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
}
