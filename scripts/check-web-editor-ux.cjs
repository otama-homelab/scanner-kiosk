const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
function fixturePDF(){
 const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Count 1 /Kids [3 0 R] >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>'];
 const stream='BT /F1 20 Tf 30 340 Td (Preview in place) Tj ET\n';objects.push('<< /Length '+Buffer.byteLength(stream)+' >>\nstream\n'+stream+'endstream','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>');
 let raw='%PDF-1.4\n';const offsets=[0];for(const [index,obj] of objects.entries()){offsets.push(Buffer.byteLength(raw));raw+=(index+1)+' 0 obj\n'+obj+'\nendobj\n'}const xref=Buffer.byteLength(raw);raw+='xref\n0 6\n0000000000 65535 f \n'+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n'+xref+'\n%%EOF\n';return Buffer.from(raw);
}
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(10000);
 const id='document_editor_0001',jobID='job_editor_0000001',errors=[],renders=[];
 let state='queued',previewFailure=true;
 const doc={id,revision:2,purpose:'document',state:'review',expires_at:new Date(Date.now()+3600000).toISOString(),active_job_id:null,output:null,pages:[1,2].map(n=>({id:'page_editor_00000'+n,ordinal:n-1,rotation:0,source_type:'pdf'}))};
 const reply=(r,json,status=200)=>r.fulfill({status,headers:{Date:new Date().toUTCString()},json});
 try{
  page.on('pageerror',e=>errors.push(e.message));await page.addInitScript(()=>document.addEventListener('securitypolicyviolation',e=>window.__csp=(window.__csp||[]).concat(e.violatedDirective)));
  await page.route('**/api/session',r=>reply(r,{authenticated:true,authenticate_type:'guest',principal_kind:'guest'}));
  await page.route('**/api/documents?*',r=>reply(r,{items:[{...doc,page_count:2}],next_cursor:null}));
  await page.route('**/api/documents/'+id,r=>reply(r,doc));
  await page.route('**/api/documents/'+id+'/pages/*/preview',r=>{if(previewFailure){previewFailure=false;return r.fulfill({status:503,body:'Unavailable'})}return r.fulfill({status:200,contentType:'application/pdf',headers:{'Content-Security-Policy':"default-src 'none'; frame-ancestors 'none'"},body:fixturePDF()})});
  await page.route('**/api/documents/'+id+'/render',r=>{renders.push({body:r.request().postDataJSON(),headers:r.request().headers()});doc.active_job_id=jobID;return reply(r,{id:jobID,document_id:id,kind:'render',state:'queued'},202)});
  await page.route('**/api/jobs/'+jobID,r=>reply(r,{id:jobID,document_id:id,kind:'render',state}));
  await page.route('**/api/documents/'+id+'/download',r=>r.fulfill({status:200,contentType:'application/pdf',headers:{'Content-Disposition':'attachment; filename="scan.pdf"'},body:Buffer.from('%PDF-1.4\n% download fixture\n')}));
  await page.setViewportSize({width:390,height:844});await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);
  await page.getByRole('heading',{name:'Document',exact:true}).waitFor();
  await page.getByRole('button',{name:'Try again',exact:true}).click();await page.waitForFunction(()=>[...document.querySelectorAll('.pdf-page canvas')].every(c=>!c.hidden&&c.width>0));
  assert.equal(await page.locator('.preview-grid canvas').count(),2,'PDF pages preview inline in the overview');
  assert.equal(await page.getByRole('button',{name:'Replace with scan',exact:true}).count(),0,'secondary page operations start collapsed');
  await page.locator('.page-more summary').first().click();await page.getByRole('button',{name:'Replace with scan',exact:true}).waitFor();
  await page.locator('.page-more summary').first().click();
  await page.getByRole('button',{name:'Save PDF',exact:true}).click();
  await page.getByText('Preparing download…',{exact:true}).waitFor();
  assert.ok(page.url().endsWith('/document/'+id),'saving stays with the preview');assert(await page.getByRole('button',{name:'Save PDF',exact:true}).isDisabled());
  assert.equal(await page.locator('.preview-grid canvas').count(),2);
  assert.equal(await page.getByText('Queued',{exact:true}).count(),0);
  assert.equal(renders.length,1);assert.equal(renders[0].headers['if-match'],'"2"');assert.match(renders[0].headers['idempotency-key'],/^rq1_/);
  await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});document.dispatchEvent(new Event('visibilitychange'))});
  const download=page.waitForEvent('download');doc.active_job_id=null;doc.output={revision:doc.revision};doc.state='ready';state='succeeded';
  await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});document.dispatchEvent(new Event('visibilitychange'))});
  assert.equal((await download).suggestedFilename(),'scan.pdf','one save operation prepares and downloads');
  await page.getByRole('link',{name:'Save PDF',exact:true}).waitFor();
  const second=page.waitForEvent('download');await page.getByRole('link',{name:'Save PDF',exact:true}).click();await second;assert.equal(renders.length,1,'fresh output downloads without another render');
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/history');await page.getByRole('heading',{name:'History',exact:true}).waitFor();
  await page.locator('.document-row').getByText('Preview',{exact:true}).click();await page.locator('.document-row canvas').waitFor();
  for(const locale of ['en','ja'])for(const theme of ['light','dark'])for(const width of [320,390,1440]){
   await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);await page.selectOption('#locale',locale);await page.getByRole('heading',{name:locale==='ja'?'書類':'Document',exact:true}).waitFor();
   await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await page.setViewportSize({width,height:844});
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'no horizontal overflow');
   if(process.env.SCANNER_TEST_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png',`-${locale}-${theme}-${width}.png`),fullPage:true});
  }
  assert.deepEqual(errors,[]);assert.deepEqual(await page.evaluate(()=>window.__csp||[]),[]);console.log(JSON.stringify({passed:true,checks:['inline PDF overview and history, explicit preview error recovery','collapsed secondary page actions','single save intent, revision and idempotency guards','preview remains while processing; visibility resume observes the same job without reposting','automatic download and fresh-output reuse','EN/JA light/dark 320/390/1440']}));
 }finally{await context.close();await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
