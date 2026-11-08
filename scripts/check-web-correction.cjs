const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const errors=[],checks=[];const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='error')errors.push('console: '+m.text())});
 let revision=1,posts=0,deletes=0,abortNext=false,risk=[],hasCorrection=true;
 const pixel=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2n1cAAAAASUVORK5CYII=','base64');
 const pageData=()=>({id:'page_correction_browser01',ordinal:0,rotation:0,source_type:'scan',capture:{size:'a4',color:'color',dpi:300},thumbnail_url:'/api/documents/document_correction_browser_001/pages/page_correction_browser01/thumbnail',preview_url:'/api/documents/document_correction_browser_001/pages/page_correction_browser01/preview',...(hasCorrection?{correction:{suggestion_id:deletes?'suggestion_correction_browser02':'suggestion_correction_browser01',correction_artifact_id:'artifact_correction_browser001',source_sha256:'a'.repeat(64),correction_sha256:'b'.repeat(64),preview_url:'/api/documents/document_correction_browser_001/pages/page_correction_browser01/correction/preview',transform:{crop_quad:[{x:0,y:0},{x:960,y:0},{x:960,y:640},{x:0,y:640}],rotation_degrees:0,output_width:960,output_height:640,confidence_kind:'unavailable'},confidence_kind:'unavailable',risk_flags:risk,state:posts?'applied':'suggested',can_undo:!!posts}}:{})});
 const doc=()=>({id:'document_correction_browser_001',revision,purpose:'document',state:'review',created_at:'2026-11-08T03:00:00Z',expires_at:new Date(Date.now()+3600000).toISOString(),page_count:1,pages:[pageData()],output:null,active_job_id:null,ready_revision:null});
 try{
  await page.route('**/api/**',async route=>{
   const request=route.request(),path=new URL(request.url()).pathname;
   if(path==='/api/session')return route.fulfill({headers:{date:new Date().toUTCString()},json:{authenticated:true,authenticate_type:'none',principal_kind:'guest',reset_required:false,version:'test'}});
   if(path==='/api/documents/document_correction_browser_001'&&request.method()==='GET')return route.fulfill({headers:{date:new Date().toUTCString()},json:doc()});
   if(path.endsWith('/correction/preview')||path.endsWith('/preview')||path.endsWith('/thumbnail'))return route.fulfill({status:200,contentType:'image/png',body:pixel});
   if(path.endsWith('/correction')&&request.method()==='POST'){
    posts++;if(abortNext){abortNext=false;return route.abort('failed')}
    const body=request.postDataJSON();assert.match(body.suggestion_id,/^suggestion_correction_browser0[12]$/);assert.equal(body.source_sha256,'a'.repeat(64));assert.match(request.headers()['idempotency-key'],/^rq1_/);
    revision=2;deletes=0;return route.fulfill({status:200,headers:{date:new Date().toUTCString(),ETag:'"2"'},json:doc()});
   }
   if(path.endsWith('/correction')&&request.method()==='DELETE'){deletes++;revision=3;posts=0;return route.fulfill({status:200,headers:{date:new Date().toUTCString(),ETag:'"3"'},json:doc()})}
   return route.fulfill({status:404,json:{code:'not_found'}});
  });
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/document_correction_browser_001');await page.locator('h1[data-document-title]').waitFor().catch(async error=>{console.error(JSON.stringify({body:await page.locator('main').innerText(),errors}));throw error});
  const compare=page.getByRole('button',{name:'Compare correction',exact:true});await compare.waitFor();assert.equal(posts,0,'loading a document must not start correction');
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))}
  await compare.click();await page.getByRole('heading',{name:'Review correction',exact:true}).waitFor();assert.equal(posts,0,'opening comparison must not start correction');
  assert.equal(await page.locator('.correction-compare img').count(),2);assert(await page.getByRole('button',{name:'Keep full original',exact:true}).isVisible());
  await page.getByRole('button',{name:'Apply correction',exact:true}).click();await page.waitForFunction(()=>document.querySelector('dialog.correction-dialog')===null);
  assert.equal(posts,1);checks.push('Explicit compare/apply; no request on view or dialog open');
  await page.getByRole('button',{name:'Compare correction',exact:true}).click();await page.getByRole('button',{name:'Undo correction',exact:true}).click();await page.waitForFunction(()=>document.querySelector('dialog.correction-dialog')===null);assert.equal(deletes,1);
  await page.getByRole('button',{name:'Compare correction',exact:true}).click();await page.getByRole('button',{name:'Apply correction',exact:true}).click();await page.waitForFunction(()=>document.querySelector('dialog.correction-dialog')===null);assert.equal(posts,1);checks.push('Apply, undo and reprocess are explicit revision-bound commands');
  abortNext=true;deletes=0;posts=0;revision=1;await page.reload();await page.locator('h1[data-document-title]').waitFor();await page.getByRole('button',{name:'Compare correction',exact:true}).click();await page.getByRole('button',{name:'Apply correction',exact:true}).click();await page.getByText('The correction result is unknown. Reload this document before retrying.',{exact:true}).waitFor();assert(await page.getByRole('button',{name:'Apply correction',exact:true}).isDisabled());checks.push('Unknown outcome blocks replay and directs reload');errors.length=0;
  risk=['edge-risk'];deletes=0;posts=0;await page.reload();await page.locator('h1[data-document-title]').waitFor();await page.getByRole('button',{name:'Compare correction',exact:true}).click();assert(await page.getByRole('button',{name:'Apply correction',exact:true}).isDisabled());await page.getByText('This suggestion has a content or confidence risk. Keep the full original or review another suggestion.',{exact:true}).waitFor();checks.push('Risk fallback preserves the full original and blocks apply');
  hasCorrection=false;await page.reload();await page.locator('h1[data-document-title]').waitFor();assert.equal(await page.getByRole('button',{name:'Compare correction',exact:true}).count(),0);checks.push('No suggestion means no correction UI');
  hasCorrection=true;risk=[];await page.evaluate(()=>localStorage.setItem('scanner.locale','ja'));await page.reload();await page.locator('h1[data-document-title]').waitFor();await page.getByRole('button',{name:'補正を比較',exact:true}).click();await page.getByRole('heading',{name:'補正を確認',exact:true}).waitFor();checks.push('Japanese labels are available');
  assert.deepEqual(errors,[]);process.stdout.write(JSON.stringify({passed:true,checks}));
 }finally{await context.close();await browser.close()}
})().catch(error=>{console.error(error.stack||error);process.exit(1)});
