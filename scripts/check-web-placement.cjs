const {reveal}=require('./web-controls.cjs');
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true,timeout:15000});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(10000);
 const errors=[],posts=[],checks=[];page.on('pageerror',e=>errors.push(e.message));
 const id='document_placement_001',target='page_placement_0002',first='page_placement_0001';
 const capture={size:'a4',color:'gray',dpi:300};
 let doc={id,revision:7,purpose:'document',state:'review',created_at:'2026-11-07T03:00:00Z',expires_at:new Date(Date.now()+3600000).toISOString(),active_job_id:null,output:null,pages:[first,target].map((id,ordinal)=>({id,ordinal,rotation:0,capture}))};
 const job={id:'job_placement_0001',document_id:id,kind:'scan',state:'queued',input_revision:8,error_code:null};
 const reply=(route,json,status=200)=>route.fulfill({status,headers:{Date:new Date().toUTCString()},json});
 try{
  await page.route('**/api/session',r=>reply(r,{authenticated:true,principal_kind:'guest',authenticate_type:'guest'}));
  await page.route('**/api/scanners',r=>reply(r,{revision:'1'.repeat(64),scanners:[{id:'scanner_placement_001',name:'Office',state:'idle',capture_options:[capture],default_capture:capture}]}));
  await page.route('**/api/documents/'+id,r=>reply(r,doc));
  await page.route('**/api/documents/'+id+'/pages/*/preview',r=>r.fulfill({status:200,contentType:'image/png',body:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6l2kAAAAASUVORK5CYII=','base64')}));
  await page.route('**/api/documents/'+id+'/pages/*/thumbnail',r=>r.fulfill({status:204}));
  await page.route('**/api/documents/'+id+'/scans',r=>{posts.push({body:r.request().postDataJSON(),headers:r.request().headers()});return reply(r,job,202)});
  await page.route('**/api/jobs/'+job.id,r=>reply(r,job));
  const open=async()=>{await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);await page.locator('h1[data-document-title]').waitFor()};
  await open();await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));if(process.env.SCANNER_TEST_PLACEMENT_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_PLACEMENT_SCREENSHOT,fullPage:true});assert.equal(await page.locator('[data-page-action=insertScan],[data-page-action=replaceScan]').count(),0);await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id+'/scan?before='+target);
  await page.getByRole('heading',{name:'Insert scan before',exact:true}).waitFor();assert.equal(posts.length,0);
  await (await reveal(page.getByRole('button',{name:'Insert scan before',exact:true,includeHidden:true}))).focus();await page.keyboard.press('Enter');await page.getByText('Reading original…',{exact:true}).waitFor();
  assert.deepEqual(posts[0].body,{scanner_id:'scanner_placement_001',capture,insert_before_page_id:target});assert.equal(posts[0].headers['if-match'],'"7"');assert.match(posts[0].headers['idempotency-key'],/^rq1_/);
  checks.push('Retained direct insertion route uses stable target ID, current revision and intent key; no scan on navigation');
  await open();await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id+'/scan?replace='+target);
  await page.getByRole('heading',{name:'Replace with scan',exact:true}).waitFor();await page.getByText('The original page is kept until the new scan succeeds.',{exact:true}).waitFor();
  await page.getByRole('button',{name:'Back',exact:true}).click();await page.locator('h1[data-document-title]').waitFor();assert.equal(posts.length,1);
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id+'/scan?replace='+target);
  await page.getByRole('heading',{name:'Replace with scan',exact:true}).waitFor();await (await reveal(page.getByRole('button',{name:'Replace with scan',exact:true,includeHidden:true}))).click();await page.getByText('Reading original…',{exact:true}).waitFor();
  assert.deepEqual(posts[1].body,{scanner_id:'scanner_placement_001',capture,replace_page_id:target});assert.notEqual(posts[0].headers['idempotency-key'],posts[1].headers['idempotency-key']);
  checks.push('Replacement explains retention and needs explicit scan; Back leaves original unchanged');
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id+'/scan');await page.getByRole('heading',{name:'Scan next page',exact:true}).waitFor();
  await page.getByRole('button',{name:'Scan next page',exact:true}).click();await page.getByText('Reading original…',{exact:true}).waitFor();assert.deepEqual(posts[2].body,{scanner_id:'scanner_placement_001',capture});
  doc={...doc,active_job_id:job.id};await open();
  for(const action of ['Insert scan before','Replace with scan'])for(const button of await page.getByRole('button',{name:action,exact:true}).all())assert(await button.isDisabled());
  doc={...doc,active_job_id:null};
  checks.push('Placement bookmarks and ordinary append work; page placement buttons are absent');
  doc={...doc,pages:[doc.pages[0]]};
  for(const suffix of ['?before='+target,'?before='+first+'&replace='+first,'?before=bad','?before='+first+'&before='+first]){
   await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id+'/scan'+suffix);await page.getByRole('button',{name:'Try again',exact:true}).waitFor();assert.equal(await page.locator('.capture-settings').count(),0);
  }
  assert.equal(posts.length,3);checks.push('Missing target, malformed and ambiguous route parameters cannot enable a scan');
  doc={...doc,purpose:'photo',photo_format:'png'};await open();
  assert.equal(await page.getByRole('button',{name:'Insert scan before',exact:true,includeHidden:true}).count(),0);
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id+'/scan?replace='+first);await page.getByRole('heading',{name:'Replace with scan',exact:true}).waitFor();
  for(const width of [320,390,768,1024,1440]){await page.setViewportSize({width,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))}
  await page.selectOption('#locale','ja');await page.getByRole('heading',{name:'スキャンで置換',exact:true}).waitFor();
  await page.getByText('新しいスキャンが成功するまで元のページを保持します。',{exact:true}).waitFor();
  checks.push('Photo replacement retains capture flow; mobile widths and Japanese guidance render without horizontal overflow');
  assert.deepEqual(errors,[]);process.stdout.write(JSON.stringify({passed:true,checks}));
 }finally{await context.close();await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
