const assert=require('node:assert/strict');
const {createHash}=require('node:crypto');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true,timeout:15000});
 const context=await browser.newContext();const page=await context.newPage();page.setDefaultTimeout(10000);
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
  const open=async()=>{await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/import');await page.getByRole('heading',{name:'Import PDF',exact:true}).waitFor();};
  await open();assert(await page.getByRole('button',{name:'Import PDF',exact:true}).isDisabled());
  for(const size of [1,55,56,63,64,65,262161]){
   await open();const data=Buffer.alloc(size);for(let i=0;i<size;i++)data[i]=(i*17+size)%256;
   await page.getByLabel('PDF file',{exact:true}).setInputFiles({name:'日本語-😀-'+('long'.repeat(20))+'.pdf',mimeType:'application/pdf',buffer:data});
   if(size===262161&&process.env.SCANNER_TEST_IMPORT_SCREENSHOT){await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:process.env.SCANNER_TEST_IMPORT_SCREENSHOT.replace('.png','-upload.png'),fullPage:true});}
   await page.getByRole('button',{name:'Import PDF',exact:true}).click();await page.getByText('Queued',{exact:true}).waitFor();
   const post=posts.at(-1);assert.equal(post.headers['x-content-sha256'],createHash('sha256').update(data).digest('hex'));assert.equal(post.headers['content-type'],'application/pdf');assert.equal(post.body.length,size);assert.deepEqual(post.body,data);assert.match(post.headers['idempotency-key'],/^rq1_/);
  }
  checks.push('Explicit bounded raw upload and SHA-256 match independent Node crypto across padding and streaming chunk boundaries');
  await open();const data=Buffer.from('%PDF-1.7\nretry');await page.getByLabel('PDF file',{exact:true}).setInputFiles({name:'retry.pdf',mimeType:'application/pdf',buffer:data});uncertain=true;
  await page.getByRole('button',{name:'Import PDF',exact:true}).click();await page.getByRole('button',{name:'Check import request',exact:true}).waitFor();assert(await page.getByLabel('PDF file',{exact:true}).isDisabled());
  await page.getByRole('button',{name:'Check import request',exact:true}).click();await page.getByText('Queued',{exact:true}).waitFor();assert.deepEqual(posts.at(-1),posts.at(-2));
  checks.push('Unknown response locks file selection and repeats the same intent key, digest and bytes');
  await open();const before=posts.length;await page.getByLabel('PDF file',{exact:true}).setInputFiles({name:'large.pdf',mimeType:'application/pdf',buffer:Buffer.alloc(1048576)});assert(await page.getByRole('button',{name:'Import PDF',exact:true}).isDisabled());assert.equal(posts.length,before);
  await page.route('**/api/documents/'+doc,r=>reply(r,{id:doc,revision:1,purpose:'document',state:'review',expires_at:new Date(Date.now()+3600000).toISOString(),pages:[{id:pdf,source_type:'pdf',rotation:90,capture:null,pdf:{width_points:200,height_points:300}},{id:scan,source_type:'scan',rotation:0,capture:{size:'a4',color:'gray',dpi:300}}]}));
  await page.route('**/pages/'+scan+'/preview',r=>r.fulfill({status:200,contentType:'image/png',body:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6l2kAAAAASUVORK5CYII=','base64')}));
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+doc);await page.getByRole('heading',{name:'Document',exact:true}).waitFor();
  const link=page.getByRole('link',{name:'Open PDF page',exact:true});assert((await link.getAttribute('href')).endsWith('/'+pdf+'/preview'));assert.equal(await link.getAttribute('target'),'_blank');assert.match(await link.getAttribute('rel'),/noopener/);assert.equal(await page.locator(`[data-page-id="${pdf}"] img`).count(),0);
  await page.getByRole('button',{name:'Page view',exact:true}).click();await page.getByRole('button',{name:'Next page',exact:true}).click();assert.equal(await page.locator('.preview-card:visible').getAttribute('data-page-id'),scan);await page.getByRole('button',{name:'Previous page',exact:true}).click();assert.equal(await page.locator('.preview-card:visible').getAttribute('data-page-id'),pdf);
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))}
  if(process.env.SCANNER_TEST_IMPORT_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_IMPORT_SCREENSHOT,fullPage:true});
  await page.selectOption('#locale','ja');await page.getByRole('link',{name:'PDFページを開く',exact:true}).waitFor();
  checks.push('PDF page uses isolated new-tab browser link without fake raster; mixed review navigation and bilingual mobile widths work');
  assert.deepEqual(errors,[]);process.stdout.write(JSON.stringify({passed:true,checks}));
 }finally{await context.close();await browser.close();}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
