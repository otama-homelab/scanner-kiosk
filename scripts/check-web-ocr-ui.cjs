const {reveal}=require('./web-controls.cjs');
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
if(process.env.SCANNER_TEST_SCENARIO){
 const {test}=require('node:test');
 const scenario=process.env.SCANNER_TEST_SCENARIO;
 test(scenario==='reocr-start'?'TestReOCRStartLifecycle':scenario==='reocr-adopt'?'TestReOCRWholeDocumentAdoption':'TestReOCRComparePositions',async(t)=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
  const checks=[],requests=[];
  try{for(const width of [1280,390]){
   const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width,height:844}});const page=await context.newPage();page.setDefaultTimeout(2500);
   const before=requests.filter(r=>r.path.endsWith('/reocr')).length;const id='document_reocr_001',jobid='job_reocr_0000001',old='result_reocr_old01',candidate='result_reocr_new01',pageid='page_reocr_000001';let active=null,cancelled=false,disposition='candidate',lose=false,stale=false;
   const summary=(rid,state)=>({id:rid,state,input_revision:1,url:'/api/documents/'+id+'/ocr-results/'+rid});
   const result=rid=>({...summary(rid,rid===old?'adopted':'candidate'),settings:{ocr:{provider_id:'remote',model_id:'model',model_version:'fixture-v1'}},pages:[{page_id:pageid,source_type:rid===old?'scan':'pdf',rotation:90,pixel_width:200,pixel_height:100,original_sha256:'a'.repeat(64),effective_sha256:(rid===old?'a':'b').repeat(64),pdf_text_origin:rid===old?'recognition':'retained-source',...(rid===old?{}:{pdf_raster:{DPI:300,SourceRotation:90}}),artifacts:Object.fromEntries(['effective','preview','text','positions'].map(kind=>[kind,'/api/documents/'+id+'/ocr-results/'+rid+'/pages/'+pageid+'/'+kind]))}]});
   const reply=(r,json,status=200)=>r.fulfill({status,headers:{Date:new Date().toUTCString()},json});
   await page.route('**/api/**',async r=>{const path=new URL(r.request().url()).pathname;requests.push({path,method:r.request().method(),body:r.request().postDataJSON(),key:r.request().headers()['idempotency-key'],match:r.request().headers()['if-match']});
    if(path==='/api/session')return reply(r,{authenticated:true,principal_kind:'guest',authenticate_type:'guest'});
    if(path==='/api/capabilities')return reply(r,{ocr_plugins:[{id:'remote',name:'Remote',external_processing:true,models:[{id:'model',name:'OCR model',available:true,outputs:['searchable_pdf'],languages:['ja'],quality_presets:['standard'],default_quality_preset:'standard',processing_destination:'Family OCR server'}]}],defaults:{ocr:{provider_id:'remote',model_id:'model',languages:['ja'],quality_preset:'standard'}}});
    if(path==='/api/documents/'+id)return reply(r,{id,revision:disposition==='adopted'||stale?2:1,purpose:'document',state:'ready',created_at:'2026-11-07T03:00:00Z',expires_at:'2099-01-01T00:00:00Z',active_job_id:active,output:{revision:disposition==='adopted'?2:1},ocr_results:scenario==='reocr-start'?[]:disposition==='candidate'?[summary(old,'adopted'),summary(candidate,'candidate')]:[summary(disposition==='adopted'?candidate:old,'adopted')],pages:[{id:pageid,rotation:0,capture:{size:'a4',color:'gray',dpi:300}}]});
    if(path.endsWith('/adopt')||path.endsWith('/discard')){if(stale)return reply(r,{code:'source_changed'},412);disposition=path.endsWith('/adopt')?'adopted':'discarded';await new Promise(resolve=>setTimeout(resolve,50));return lose?r.abort('failed'):reply(r,{id,revision:disposition==='adopted'?2:1})}
    if(path.endsWith('/ocr-results/'+old)||path.endsWith('/ocr-results/'+candidate))return reply(r,{...result(path.endsWith(old)?old:candidate),state:path.endsWith(candidate)?disposition:'adopted'});
    if(path.includes('/ocr-results/')&&path.endsWith('/positions'))return reply(r,{page_id:pageid,width:200,height:100,spans:[{text:'<script>window.ocrInjected=true</script>',box:{x:20,y:10,width:40,height:20}},{text:'日本語 second',box:{x:100,y:50,width:60,height:10}}]});
    if(path.includes('/ocr-results/')&&path.endsWith('/text'))return r.fulfill({status:200,contentType:'text/plain',body:'<script>window.ocrInjected=true</script>\n日本語 second'});
    if(path.endsWith('/preview')||path.endsWith('/effective')||path.endsWith('/thumbnail'))return r.fulfill({status:200,contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><rect width="200" height="100" fill="white"/><path d="M20 10h40v20H20z" fill="black"/></svg>'});
    if(path.endsWith('/reocr')){active=jobid;return reply(r,{id:jobid,document_id:id,kind:'render',state:'queued',input_revision:1},202)}
    if(path.endsWith('/cancel')){cancelled=true;return reply(r,{id:jobid,document_id:id,kind:'render',state:'cancelled',input_revision:1})}
    if(path==='/api/jobs/'+jobid)return reply(r,{id:jobid,document_id:id,kind:'render',state:cancelled?'cancelled':'queued',input_revision:1});
    return r.fulfill({status:404,json:{}});
   });
   page.on('console',e=>{if(e.type()==='error')console.error(e.text())});page.on('pageerror',e=>console.error(e.message));await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);try{await page.locator('h1[data-document-title]').waitFor()}catch(e){console.error(await page.locator('body').innerText(),requests);throw e}
   assert.equal(requests.filter(r=>r.path.endsWith('/reocr')).length,before);
   if(scenario==='reocr-adopt'){
    const adopt=page.getByRole('button',{name:'Adopt for whole document',exact:true});assert.equal(await adopt.count(),1,'candidate requires explicit whole-document adoption');
    assert.equal(requests.filter(r=>r.path.endsWith('/adopt')).length,width===1280?0:2,'opening never adopts');
    await adopt.click();await page.waitForFunction(()=>document.querySelector('[data-reocr-action]')===document.activeElement);assert.equal(disposition,'adopted');assert.equal(await adopt.count(),0);assert(await page.locator('a.download-link').isVisible());
    const command=requests.filter(r=>r.path.endsWith('/adopt')).at(-1);assert.deepEqual(command.body,{});assert.equal(command.match,'"1"');assert.match(command.key,/^rq1_/);
    await t.test('TestReOCRDiscardRecovery '+width,async()=>{
     disposition='candidate';lose=true;await page.reload();await page.getByRole('button',{name:'Discard candidate',exact:true}).click();await page.getByRole('button',{name:'Check recognition status',exact:true}).click();await page.getByRole('button',{name:'Discard candidate',exact:true}).waitFor({state:'detached'});assert.equal(disposition,'discarded');assert(await page.locator('a.download-link').isVisible());assert.equal(requests.filter(r=>r.path.endsWith('/discard')).length,width===1280?1:2,'lost disposition response must not duplicate command');
     disposition='candidate';lose=false;stale=true;await page.reload();await page.getByRole('button',{name:'Adopt for whole document',exact:true}).click();await page.getByText('The document changed. Reload before trying again.',{exact:true}).waitFor();assert(await page.locator('a.download-link').isVisible());stale=false;
    });
    await t.test('TestReOCRKeyboardFocus '+width,async()=>{
     disposition='adopted';await page.reload();const action=page.getByRole('button',{name:'Recognize text again',exact:true});await action.click();await page.keyboard.press('Escape');assert(await action.evaluate(e=>document.activeElement===e));await action.click();await page.getByRole('button',{name:'Close',exact:true}).click();assert(await action.evaluate(e=>document.activeElement===e));
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
   await page.getByRole('button',{name:'Cancel recognition',exact:true}).click();await page.getByText('cancelled',{exact:true}).waitFor();assert(await page.locator('a.download-link').isVisible());
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
