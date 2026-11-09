const {reveal}=require('./web-controls.cjs');
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
if(process.env.SCANNER_TEST_SCENARIO){
 const {test}=require('node:test');
 test('TestReOCRStartLifecycle',async()=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
  const checks=[],requests=[];
  try{for(const width of [1280,390]){
   const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width,height:844}});const page=await context.newPage();page.setDefaultTimeout(2500);
   const id='document_reocr_001',jobid='job_reocr_0000001';let active=null,cancelled=false;
   const reply=(r,json,status=200)=>r.fulfill({status,headers:{Date:new Date().toUTCString()},json});
   await page.route('**/api/**',async r=>{const path=new URL(r.request().url()).pathname;requests.push({path,method:r.request().method(),body:r.request().postDataJSON()});
    if(path==='/api/session')return reply(r,{authenticated:true,principal_kind:'guest',authenticate_type:'guest'});
    if(path==='/api/capabilities')return reply(r,{ocr_plugins:[{id:'remote',name:'Remote',external_processing:true,models:[{id:'model',name:'OCR model',available:true,outputs:['searchable_pdf'],languages:['ja'],quality_presets:['standard'],default_quality_preset:'standard',processing_destination:'Family OCR server'}]}],defaults:{ocr:{provider_id:'remote',model_id:'model',languages:['ja'],quality_preset:'standard'}}});
    if(path==='/api/documents/'+id)return reply(r,{id,revision:1,purpose:'document',state:'ready',created_at:'2026-11-07T03:00:00Z',expires_at:'2099-01-01T00:00:00Z',active_job_id:active,output:{revision:1},pages:[{id:'page_reocr_000001',rotation:0,capture:{size:'a4',color:'gray',dpi:300}}]});
    if(path.endsWith('/reocr')){active=jobid;return reply(r,{id:jobid,document_id:id,kind:'reocr',state:'queued',input_revision:1},202)}
    if(path.endsWith('/cancel')){cancelled=true;return reply(r,{id:jobid,document_id:id,kind:'reocr',state:'cancelled',input_revision:1})}
    if(path==='/api/jobs/'+jobid)return reply(r,{id:jobid,document_id:id,kind:'reocr',state:cancelled?'cancelled':'queued',input_revision:1});
    return r.fulfill({status:404,json:{}});
   });
   page.on('console',e=>{if(e.type()==='error')console.error(e.text())});page.on('pageerror',e=>console.error(e.message));await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);try{await page.locator('h1[data-document-title]').waitFor()}catch(e){console.error(await page.locator('body').innerText(),requests);throw e}
   assert.equal(requests.filter(r=>r.path.endsWith('/reocr')).length,0);
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
