const assert=require('node:assert/strict');
const {createHash}=require('node:crypto');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true,timeout:15000});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(10000);
 const errors=[],posts=[],checks=[];page.on('pageerror',e=>errors.push(e.message));let uncertain=false;
 const doc='document_import_ui1',pdf='page_import_ui0001',scan='page_import_scan01';
 const job={id:'job_import_ui00001',document_id:doc,kind:'import',state:'queued',input_revision:1,error_code:null};
 const reply=(r,json,status=200)=>r.fulfill({status,headers:{Date:new Date().toUTCString()},json});
 try {
  await page.route('**/api/session',r=>reply(r,{authenticated:true,principal_kind:'guest',authenticate_type:'guest'}));
  await page.route('**/api/capabilities',r=>reply(r,{limits:{job_max_bytes:1048576}}));
  await page.route('**/api/scanners',r=>reply(r,{scanners:[]}));
  await page.route('**/api/jobs/'+job.id,r=>reply(r,job));
  await page.route('**/api/documents/import',r=>{const request=r.request();posts.push({headers:request.headers(),body:request.postDataBuffer()});if(uncertain){uncertain=false;return r.abort('failed')}return reply(r,job,202)});
  await page.goto(process.env.SCANNER_TEST_ORIGIN);await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  const choosing=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Import PDF',exact:true}).click();const chooser=await choosing;
  assert(!page.url().endsWith('#/import'),'opening the native picker must not navigate to an import page');assert.equal(posts.length,0);
  await chooser.setFiles({name:'direct.pdf',mimeType:'application/pdf',buffer:Buffer.from('%PDF-1.7\ndirect')});
  await page.getByText('Importing PDF…',{exact:true}).waitFor();assert.equal(posts.length,1,'file selection submits once without another button');
  await page.route('**/api/documents/'+doc,r=>reply(r,{id:doc,revision:1,purpose:'document',state:'review',created_at:'2026-11-07T03:00:00Z',expires_at:new Date(Date.now()+3600000).toISOString(),pages:[]}));
  job.state='succeeded';await page.locator('h1[data-document-title]').waitFor();
  assert(page.url().endsWith('#/document/'+doc),'completed direct import opens the existing document editor');job.state='queued';
  checks.push('Home opens native file picker; selection uploads once and completion opens document editor without an intermediate import page');
  const open=async()=>{await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/import');await page.getByRole('heading',{name:'Import PDF',exact:true}).waitFor();};
  await open();assert(await page.getByRole('button',{name:'Import PDF',exact:true}).isDisabled());
  for(const size of [1,55,56,63,64,65,262161]){
   await open();const data=Buffer.alloc(size);for(let i=0;i<size;i++)data[i]=(i*17+size)%256;
   await page.getByLabel('PDF file',{exact:true}).setInputFiles({name:'日本語-😀-'+('long'.repeat(20))+'.pdf',mimeType:'application/pdf',buffer:data});
   if(size===262161&&process.env.SCANNER_TEST_IMPORT_SCREENSHOT){await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:process.env.SCANNER_TEST_IMPORT_SCREENSHOT.replace('.png','-upload.png'),fullPage:true});}
   await page.getByRole('button',{name:'Import PDF',exact:true}).click();await page.getByText('Importing PDF…',{exact:true}).waitFor();
   const post=posts.at(-1);assert.equal(post.headers['x-content-sha256'],createHash('sha256').update(data).digest('hex'));assert.equal(post.headers['content-type'],'application/pdf');assert.equal(post.body.length,size);assert.deepEqual(post.body,data);assert.match(post.headers['idempotency-key'],/^rq1_/);
  }
  checks.push('Explicit bounded raw upload and SHA-256 match independent Node crypto across padding and streaming chunk boundaries');
  await open();const data=Buffer.from('%PDF-1.7\nretry');await page.getByLabel('PDF file',{exact:true}).setInputFiles({name:'retry.pdf',mimeType:'application/pdf',buffer:data});uncertain=true;
  await page.getByRole('button',{name:'Import PDF',exact:true}).click();await page.getByRole('button',{name:'Check import request',exact:true}).waitFor();assert(await page.getByLabel('PDF file',{exact:true}).isDisabled());
  await page.getByRole('button',{name:'Check import request',exact:true}).click();await page.getByText('Importing PDF…',{exact:true}).waitFor();assert.deepEqual(posts.at(-1),posts.at(-2));
  checks.push('Unknown response locks file selection and repeats the same intent key, digest and bytes');
  for(const [code,message] of [['pdf_invalid','This PDF is damaged or could not be read.'],['pdf_encrypted','Password-protected PDFs cannot be imported. Save an unencrypted copy first.'],['pdf_unsupported','This PDF contains features that cannot be imported.'],['pdf_limit','This PDF exceeds the page or processing limits.']]){
   job.state='failed';job.error_code=code;await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/job/'+job.id);await page.reload();await page.getByText(message,{exact:true}).waitFor();assert.equal(new URL(page.url()).hash,'','failed import returns home');await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  }
  for(const code of ['private-provider-path-token','__proto__','constructor']){job.error_code=code;await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/job/'+job.id);await page.reload();await page.getByText('The request could not be completed. Try again.',{exact:true}).waitFor();assert(!(await page.locator('body').innerText()).includes(code))}job.state='queued';job.error_code=null;
  checks.push('Failed imports return home with allowlisted reasons; private unknown errors stay hidden');
  await open();const before=posts.length;await page.getByLabel('PDF file',{exact:true}).setInputFiles({name:'large.pdf',mimeType:'application/pdf',buffer:Buffer.alloc(1048576)});assert(await page.getByRole('button',{name:'Import PDF',exact:true}).isDisabled());assert.equal(posts.length,before);
  await page.route('**/api/documents/'+doc,r=>reply(r,{id:doc,revision:1,purpose:'document',state:'review',created_at:'2026-11-07T03:00:00Z',expires_at:new Date(Date.now()+3600000).toISOString(),pages:[{id:pdf,source_type:'pdf',rotation:90,capture:null,pdf:{width_points:200,height_points:300}},{id:scan,source_type:'scan',rotation:0,capture:{size:'a4',color:'gray',dpi:300}}]}));
  await page.route('**/pages/'+scan+'/preview',r=>r.fulfill({status:200,contentType:'image/png',body:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6l2kAAAAASUVORK5CYII=','base64')}));
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+doc);await page.locator('h1[data-document-title]').waitFor();
  const link=page.getByRole('link',{name:'Open PDF page',exact:true});assert((await link.getAttribute('href')).endsWith('/'+pdf+'/preview'));assert.equal(await link.getAttribute('target'),'_blank');assert.match(await link.getAttribute('rel'),/noopener/);assert.equal(await page.locator(`[data-page-id="${pdf}"] img`).count(),0);
  assert.equal(await page.locator('.review-view-switch').count(),0);assert.equal(await page.locator('.preview-card:visible').count(),2);
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))}
  if(process.env.SCANNER_TEST_IMPORT_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_IMPORT_SCREENSHOT,fullPage:true});
  await page.selectOption('#locale','ja');await page.getByRole('link',{name:'PDFページを開く',exact:true}).waitFor();
  checks.push('PDF page uses isolated new-tab browser link without fake raster; mixed review navigation and bilingual mobile widths work');
  assert.deepEqual(errors,[]);process.stdout.write(JSON.stringify({passed:true,checks}));
 }finally{await context.close();await browser.close();}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
