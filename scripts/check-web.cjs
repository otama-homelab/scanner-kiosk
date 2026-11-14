const {reveal}=require('./web-controls.cjs');
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 let debug=async()=>({});
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();const errors=[],responses=[],failed=[];page.on('response',r=>responses.push([new URL(r.url()).pathname,r.status()]));page.on('requestfailed',r=>failed.push([new URL(r.url()).pathname,r.failure()]));
  // Pace this multi-scenario run below the default 10 requests/s API budget.
  // Explicit 429 recovery is tested separately by check-web-rate.cjs.
  let nextRequest=0;
  await context.route('**/*',async route=>{
   const path=new URL(route.request().url()).pathname;
   if(path.startsWith('/api/')||path.startsWith('/auth/')){const now=performance.now();const delay=Math.max(0,nextRequest-now);nextRequest=Math.max(now,nextRequest)+110;if(delay)await new Promise(resolve=>setTimeout(resolve,delay))}
   await route.continue();
  });
  debug=async()=>({url:page.url(),body:(await page.locator('body').innerText()).slice(0,1500),errors,responses:responses.slice(-40),failed});
  page.on('pageerror',error=>errors.push(error.message));await page.addInitScript(()=>document.addEventListener('securitypolicyviolation',e=>window.__csp=(window.__csp||[]).concat(e.violatedDirective)));
  await page.goto(process.env.SCANNER_TEST_ORIGIN);
  await page.getByRole('button',{name:'Start session',exact:true}).waitFor({timeout:5000}).catch(async error=>{console.error(JSON.stringify({body:await page.locator('body').innerText(),errors,responses,failed,csp:await page.evaluate(()=>window.__csp||[])}));throw error});
  const runningVersion=(await (await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/session')).json()).version;assert.equal(typeof runningVersion,'string');assert(runningVersion.length>0);assert.equal(await page.locator('footer#app-version').count(),1);assert.equal(await page.locator('footer#app-version').innerText(),runningVersion);
  if(process.env.SCANNER_TEST_SCREENSHOT){const viewport=page.viewportSize();await page.setViewportSize({width:390,height:844});await page.locator('[data-theme-choice=dark]').click();await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-footer-dark.png'),fullPage:true,animations:'disabled'});await page.locator('[data-theme-choice=system]').click();await page.setViewportSize(viewport)}
  await page.getByRole('button',{name:'Start session',exact:true}).click();
  await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  await page.getByRole('button',{name:'History',exact:true}).click();await page.getByText('No documents',{exact:true}).waitFor();
  const seed=await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/seed');assert.equal(seed.status(),200);
  await page.reload();await page.getByRole('button',{name:/^Open(?: |$)/}).click();
  await page.waitForFunction(()=>document.querySelector('.preview-card img')?.naturalWidth===32);
  assert.equal(await page.evaluate(()=>document.activeElement.tagName),'H1','document navigation must focus its heading');
  const editRequests=[];page.on('request',request=>{if(['PATCH','PUT','DELETE'].includes(request.method()))editRequests.push(request.url())});
  assert.equal(await page.locator('.review-view-switch').count(),0,'one overview without redundant mode tabs');
  assert.equal(await page.locator('.preview-card:visible').count(),1);
  assert.equal(await page.locator('.page-more').count(),0,'page actions stay visible');
  for(const action of await page.locator('.page-actions button').all()){assert.ok(await action.getAttribute('aria-label'));assert.equal(await action.getAttribute('title'),await action.getAttribute('aria-label'))}
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`overview overflow at ${width}`)}
  await page.setViewportSize({width:390,height:844});await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-page-mobile.png'),fullPage:true});
  assert.deepEqual(editRequests,[],'opening the overview must not mutate pages');
  const ordered=()=>page.locator('.preview-card img').evaluateAll(images=>images.map(image=>image.getAttribute('src').split('/').at(-2)));
  assert.deepEqual(await ordered(),['page_browser_0001','page_browser_0002']);
  const focusedPageAction=()=>page.evaluate(()=>({page:document.activeElement.closest('[data-page-id]')?.getAttribute('data-page-id'),action:document.activeElement.getAttribute('data-page-action')}));
  await page.locator('.document-pagebar select').selectOption('page_browser_0002');assert.deepEqual(editRequests,[],'page selection never mutates pages');
  await (await reveal(page.locator('.preview-card').nth(1).getByRole('button',{name:'Move earlier',exact:true,includeHidden:true}))).focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('.preview-card img')?.getAttribute('src')?.includes('page_browser_0002'));assert.deepEqual(await focusedPageAction(),{page:'page_browser_0002',action:'rotate'});assert.equal(await page.locator('.preview-card:visible').count(),1);assert.equal(await page.locator('.preview-card').first().getAttribute('data-page-id'),'page_browser_0002');assert.equal(await page.locator('.review-view-switch').count(),0);await page.reload();await page.locator('h1[data-document-title]').waitFor();assert.deepEqual(await ordered(),['page_browser_0002','page_browser_0001']);
  await (await reveal(page.locator('.preview-card').first().getByRole('button',{name:'Move later',exact:true,includeHidden:true}))).click();await page.waitForFunction(()=>document.querySelector('.preview-card img')?.getAttribute('src')?.includes('page_browser_0001'));assert.deepEqual(await focusedPageAction(),{page:'page_browser_0002',action:'rotate'});
  await (await reveal(page.locator('.preview-card').nth(1).getByRole('button',{name:'Delete page',exact:true,includeHidden:true}))).click();await (await reveal(page.getByRole('dialog').getByRole('button',{name:'Delete page',exact:true,includeHidden:true}))).click();await page.waitForFunction(()=>document.querySelectorAll('.preview-card').length===1&&document.querySelector('#main')?.getAttribute('aria-busy')==='false');
  assert.equal(await page.evaluate(()=>document.activeElement.tagName),'H1','deleted page must restore document heading focus');assert.equal(await page.locator('.preview-card:visible').getAttribute('data-page-id'),'page_browser_0001');assert.equal(await page.locator('.review-view-switch').count(),0);
  const pageRoute=process.env.SCANNER_TEST_ORIGIN+'/api/documents/document_browser_01/pages/page_browser_0001';let rejectedEdits=0;
  await page.route(pageRoute,route=>{rejectedEdits++;return route.fulfill({status:429,json:{code:'rate_limited'},headers:{Date:new Date().toUTCString(),'Retry-After':'1'}})});
  await page.getByRole('button',{name:'Rotate',exact:true}).focus();await page.keyboard.press('Enter');await page.getByText('Too many requests. Wait 1s, then try again.',{exact:true}).waitFor();assert.deepEqual(await focusedPageAction(),{page:'page_browser_0001',action:'rotate'});assert.equal(rejectedEdits,1);
  await page.unroute(pageRoute);await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('.preview-card img')?.style.transform.endsWith('rotate(90deg)'));assert.deepEqual(await focusedPageAction(),{page:'page_browser_0001',action:'rotate'});assert.equal(await page.locator('.review-view-switch').count(),0);
  await page.waitForFunction(()=>document.querySelector('.preview-card img')?.naturalWidth===32);
  await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-edit-focus.png'),fullPage:true});
  const delayedRotation=async during=>{
   let release,arrived;const held=new Promise(resolve=>release=resolve);const seen=new Promise(resolve=>arrived=resolve);
   await page.route(pageRoute,async route=>{const response=await route.fetch();arrived(response.status());await held;await route.fulfill({response})});
   try{
    await page.getByRole('button',{name:'Rotate',exact:true}).focus();await page.keyboard.press('Enter');assert.equal(await seen,200,'held edit must really commit');
    const completed=page.waitForResponse(response=>response.url()===pageRoute&&response.request().method()==='PATCH');
    await during();release();const response=await completed;await response.finished();await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(resolve)));
   }finally{release();await page.unroute(pageRoute)}
  };
  await delayedRotation(()=>page.locator('#locale').focus());await page.waitForFunction(()=>document.querySelector('.preview-card img')?.style.transform.endsWith('rotate(180deg)'));
  assert.equal(await page.evaluate(()=>document.activeElement.id),'locale','late edit must preserve newer header focus');
  await delayedRotation(async()=>{await page.locator('#locale').focus();await page.selectOption('#locale','ja');await page.locator('h1[data-document-title]').waitFor()});
  assert.equal(await page.locator('html').getAttribute('lang'),'ja');assert.equal(await page.evaluate(()=>document.activeElement.id),'locale','late edit must not override a newer locale render');
  // The next edit uses the now-localized action but the same committed page.
  await page.selectOption('#locale','en');await page.locator('h1[data-document-title]').waitFor();
  await delayedRotation(async()=>{await page.locator('#history').click();await page.getByRole('heading',{name:'History',exact:true}).waitFor()});
  assert.ok(page.url().endsWith('#/history'));assert.equal(await page.evaluate(()=>document.activeElement.textContent),'History','late edit must not restore the previous document view');
  await page.getByRole('button',{name:/^Open(?: |$)/}).click();await page.getByRole('button',{name:'Rotate',exact:true}).click();await page.waitForFunction(()=>document.querySelector('.preview-card img')?.style.transform.endsWith('rotate(90deg)'));
  assert.equal(await (await reveal(page.getByRole('button',{name:'Move earlier',exact:true,includeHidden:true}))).isDisabled(),true);assert.equal(await (await reveal(page.getByRole('button',{name:'Move later',exact:true,includeHidden:true}))).isDisabled(),true);
  assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/job')).status(),200);
  await page.reload();await page.getByText('Preparing download…',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Rotate',exact:true}).isDisabled(),true);assert(await page.getByRole('button',{name:'Save PDF',exact:true}).isDisabled());
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/job/job_browser_00001');await page.getByText('Preparing download…',{exact:true}).waitFor();
  await page.locator('#history').click();await page.getByRole('link',{name:'View processing status',exact:true}).click();
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/document_browser_01/scan');await page.getByText('Preparing download…',{exact:true}).waitFor();assert.ok(page.url().endsWith('/job/job_browser_00001'),'active document scan route must recover its job, not propose another capture');

  await page.getByText('Preparing download…',{exact:true}).waitFor();await page.keyboard.press('Tab');await page.getByRole('button',{name:'Cancel',exact:true}).focus();
  await page.waitForResponse(r=>new URL(r.url()).pathname==='/api/jobs/job_browser_00001'&&r.request().method()==='GET');
  assert.equal(await page.evaluate(()=>document.activeElement.textContent),'Cancel','polling must preserve focus');
  const jobRoute=process.env.SCANNER_TEST_ORIGIN+'/api/jobs/job_browser_00001';let polledState='running';
  await page.route(jobRoute,route=>route.fulfill({json:{id:'job_browser_00001',document_id:'document_browser_01',kind:'render',state:polledState},headers:{Date:new Date().toUTCString()}}));
  await page.getByText('Preparing download…',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>document.activeElement.textContent),'Cancel','changed job state must preserve equivalent action focus');
  await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-job-running-focus.png'),fullPage:true});
  polledState='cancelling';await page.getByText('Cancelling',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>document.activeElement.tagName),'H1','removed job action must focus status heading');
  await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-job-cancelling-focus.png'),fullPage:true});
  await page.locator('#locale').focus();polledState='cancelled';await page.getByText('Cancelled',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>document.activeElement.id),'locale','job state update must preserve header focus');
  await page.unroute(jobRoute);await page.reload();await page.getByText('Preparing download…',{exact:true}).waitFor();
  await page.getByRole('button',{name:'Cancel',exact:true}).click();await page.getByText('Cancelling',{exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>document.activeElement.tagName),'H1','cancel completion must focus status heading');
  assert.equal(await page.getByRole('button',{name:'Cancel',exact:true}).count(),0);
  await page.reload();await page.getByText('Cancelling',{exact:true}).waitFor();
  assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/job-finish')).status(),200);
  await page.getByText('Cancelled',{exact:true}).waitFor();
  const before=responses.filter(r=>r[0]==='/api/jobs/job_browser_00001').length;
  // Wait beyond one configured polling interval to detect accidental terminal polling.
  await page.waitForTimeout(2300);assert.equal(responses.filter(r=>r[0]==='/api/jobs/job_browser_00001').length,before);
  await page.getByRole('button',{name:/^Open(?: |$)/}).click();await page.locator('h1[data-document-title]').waitFor();
  const automaticPDF=page.waitForEvent('download');await page.getByRole('button',{name:'Save PDF',exact:true}).click();await page.getByText('Preparing download…',{exact:true}).waitFor();
  assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/run-worker')).status(),200);
  await automaticPDF;await page.locator('h1[data-document-title]').waitFor();
  assert.equal(await page.getByRole('button',{name:'Save PDF',exact:true}).count(),0);
  const downloadEvent=page.waitForEvent('download');await page.getByRole('link',{name:'Save PDF',exact:true}).click();const download=await downloadEvent;
  assert.equal(download.suggestedFilename(),'scan.pdf');const pdf=require('node:fs').readFileSync(await download.path());assert.equal(pdf.subarray(0,4).toString(),'%PDF');
  await page.waitForFunction(()=>document.querySelector('.preview-card img')?.getAttribute('src')?.endsWith('/thumbnail')&&document.querySelector('.preview-card img')?.naturalWidth>0);
  assert.equal(await page.locator('.review-view-switch').count(),0);
  const gridBefore=await page.locator('.preview-grid').boundingBox();await (await reveal(page.getByRole('button',{name:'Output settings',exact:true,includeHidden:true}))).click();
  await page.getByRole('dialog').waitFor();assert.deepEqual(await page.locator('.preview-grid').boundingBox(),gridBefore,'dialog must not resize content');
  await page.mouse.click(3,3);assert.equal(await page.getByRole('dialog').count(),0);
  await (await reveal(page.getByRole('button',{name:'Output settings',exact:true,includeHidden:true}))).click();await page.keyboard.press('Escape');assert.equal(await page.getByRole('dialog').count(),0);
  await (await reveal(page.getByRole('button',{name:'Output settings',exact:true,includeHidden:true}))).click();await page.selectOption('#output-choice','lossless');await page.getByRole('dialog').getByRole('button',{name:'Save PDF',exact:true}).click();await page.getByText('Preparing download…',{exact:true}).waitFor();
  assert.deepEqual(await (await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/__fixture/latest-render')).json(),{format:'pdf',pdf_encoding:'lossless'});
  const losslessDownload=page.waitForEvent('download');assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/run-worker')).status(),200);await losslessDownload;await page.locator('h1[data-document-title]').waitFor();
  assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/photo')).status(),200);await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/photo_browser_0001');await page.locator('h1[data-document-title]').waitFor();
  await (await reveal(page.getByRole('button',{name:'Output settings',exact:true,includeHidden:true}))).click();await page.selectOption('#output-choice','png');await page.getByRole('dialog').getByRole('button',{name:'Save image',exact:true}).click();await page.getByText('Preparing download…',{exact:true}).waitFor();
  assert.deepEqual(await (await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/__fixture/latest-render')).json(),{format:'png',pdf_encoding:null});
  const automaticPNG=page.waitForEvent('download');assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/run-worker')).status(),200);await automaticPNG;await page.locator('h1[data-document-title]').waitFor();
  const pngEvent=page.waitForEvent('download');await page.getByRole('link',{name:'Save image',exact:true}).click();const pngDownload=await pngEvent;assert.equal(pngDownload.suggestedFilename(),'scan.png');assert.equal(require('node:fs').readFileSync(await pngDownload.path()).subarray(0,8).toString('hex'),'89504e470d0a1a0a');
  assert.equal(await page.locator('.photo-review').count(),1);assert.equal(await page.locator('.page-actions button').count(),2,'photo has only rotate and delete');assert.equal(await page.getByRole('button',{name:'Scan next page',exact:true}).count(),0);
  const documentDeletes=[];const photoURL=process.env.SCANNER_TEST_ORIGIN+'/api/documents/photo_browser_0001';
  page.on('request',request=>{if(request.method()==='DELETE'&&request.url()===photoURL)documentDeletes.push(request.headers())});
  await (await reveal(page.getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();assert.equal(await page.evaluate(()=>document.activeElement.textContent),'Cancel');for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`delete dialog overflow at ${width}`)}await page.setViewportSize({width:390,height:844});await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-delete-mobile.png'),fullPage:true});await page.keyboard.press('Escape');assert.equal(documentDeletes.length,0);
  await (await reveal(page.getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();assert.equal(documentDeletes.length,0);
  let releaseDelete;const deleteGate=new Promise(resolve=>releaseDelete=resolve);let lostDelete=true;
  await page.route(photoURL,async route=>{if(route.request().method()!=='DELETE')return route.continue();if(lostDelete){lostDelete=false;const committed=await route.fetch();assert.equal(committed.status(),202);await deleteGate;return route.fulfill({status:503,contentType:'application/json',body:'{"code":"storage_unavailable"}'})}return route.continue()});
  await (await reveal(page.getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();await (await reveal(page.getByRole('dialog').getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();
  await page.waitForFunction(()=>document.querySelector('.deletion-dialog')?.getAttribute('aria-busy')==='true');await page.keyboard.press('Escape');assert.equal(await page.getByRole('dialog').count(),1);assert.equal(await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).isDisabled(),true);releaseDelete();
  await page.getByText('Deletion may have started. Check the request again before leaving this screen.',{exact:true}).waitFor();await page.getByRole('dialog').getByRole('button',{name:'Check deletion request',exact:true}).click();
  await page.getByRole('heading',{name:'History',exact:true}).waitFor();assert.equal(await page.locator('.document-row[data-document-id=photo_browser_0001]').count(),0);assert.equal(documentDeletes.length,2);assert.equal(documentDeletes[0]['idempotency-key'],documentDeletes[1]['idempotency-key']);assert.equal(documentDeletes[0]['if-match'],documentDeletes[1]['if-match']);
  assert.equal(await page.getByRole('button',{name:'Cancel',exact:true}).count(),0);assert.equal(await page.getByRole('button',{name:/^Open(?: |$)/}).count(),0);assert.equal(await page.locator('.preview-card').count(),0);assert.equal((await page.request.get(photoURL)).status(),404);
  await page.reload();await page.getByRole('heading',{name:'History',exact:true}).waitFor();assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/run-worker')).status(),200);assert.equal(await page.getByText('Deleted',{exact:true}).count(),0);
  await page.unroute(photoURL);
  assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/history-delete')).status(),200);
  await page.locator('#history').click();const historyPhoto=page.locator('.document-row[data-document-id=document_history_delete_01]');await (await reveal(historyPhoto.getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();
  let conflictOnce=true;let historyAccepted,releaseHistory;const acceptedHistory=new Promise(resolve=>historyAccepted=resolve);const historyGate=new Promise(resolve=>releaseHistory=resolve);const historyURL=process.env.SCANNER_TEST_ORIGIN+'/api/documents/document_history_delete_01';
  await page.route(historyURL,async route=>{if(route.request().method()!=='DELETE')return route.continue();if(conflictOnce){conflictOnce=false;const changed=await page.request.patch(historyURL+'/pages/page_history_delete_01',{headers:{Origin:process.env.SCANNER_TEST_ORIGIN,'If-Match':'"1"','Idempotency-Key':'browser_delete_stale_01'},data:{rotation:90}});assert.equal(changed.status(),200);return route.continue()}const committed=await route.fetch();assert.equal(committed.status(),202);historyAccepted(await committed.json());await historyGate;await route.fulfill({response:committed}).catch(()=>{})});
  await (await reveal(page.getByRole('dialog').getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();await page.getByText('This document changed. Reload it before editing.',{exact:true}).waitFor();assert.equal(await page.evaluate(()=>document.activeElement.textContent),'Reload document');assert.equal(await page.getByRole('dialog').getByRole('button',{name:'Delete document',exact:true}).count(),0);await page.getByRole('button',{name:'Reload document',exact:true}).click();
  await (await reveal(historyPhoto.getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();await (await reveal(page.getByRole('dialog').getByRole('button',{name:'Delete document',exact:true,includeHidden:true}))).click();const historyCleanup=await acceptedHistory;
  await page.evaluate(()=>location.hash='');await page.getByRole('heading',{name:'Home',exact:true}).waitFor();releaseHistory();await page.waitForTimeout(200);assert.equal(new URL(page.url()).hash,'');assert.equal(await page.getByRole('heading',{name:'Home',exact:true}).count(),1,'late deletion response must not redirect a newer view');
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/job/'+historyCleanup.id);await page.getByRole('heading',{name:'History',exact:true}).waitFor();assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/run-worker')).status(),200);assert.equal(await page.getByText('Deleted',{exact:true}).count(),0);await page.unroute(historyURL);
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/document_browser_01');await page.locator('h1[data-document-title]').waitFor();
  await page.selectOption('#locale','ja');await page.locator('h1[data-document-title]').waitFor();await (await reveal(page.getByRole('button',{name:'書類を削除',exact:true,includeHidden:true}))).click();await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-delete-ja-mobile.png'),fullPage:true});await page.getByRole('dialog').getByRole('button',{name:'中止',exact:true}).click();
  await page.getByRole('button',{name:'ダーク',exact:true}).click();assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`overflow at ${width}`)}
  await page.reload();await page.locator('h1[data-document-title]').waitFor();assert.equal(await page.locator('html').getAttribute('lang'),'ja');assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');
  await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT,fullPage:true});await page.setViewportSize({width:390,height:844});await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-mobile.png'),fullPage:true});await (await reveal(page.getByRole('button',{name:'出力設定',exact:true,includeHidden:true}))).click();await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-output-mobile.png'),fullPage:true});await page.keyboard.press('Escape');
  // OCR projection is fixture-authored; admission is real, provider execution is not injected.
  await (await reveal(page.getByRole('button',{name:'出力設定',exact:true,includeHidden:true}))).click();
  await page.getByText('文字認識を利用できません。OCRなしのPDF保存は選べます。',{exact:true}).waitFor();assert.equal(await page.locator('#ocr-enabled').isDisabled(),true);assert.equal(await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).isEnabled(),true);await page.keyboard.press('Escape');
  assert.equal((await page.request.post(process.env.SCANNER_TEST_ORIGIN+'/__fixture/ocr-catalog')).status(),200);
  const missingDestination=async route=>{const response=await route.fetch();const metadata=await response.json();for(const provider of metadata.ocr_plugins??[])if(provider.external_processing)for(const model of provider.models)delete model.processing_destination;await route.fulfill({response,json:metadata})};
  await page.route('**/api/capabilities',missingDestination);await (await reveal(page.getByRole('button',{name:'出力設定',exact:true,includeHidden:true}))).click();await page.getByText('文字認識を利用できません。OCRなしのPDF保存は選べます。',{exact:true}).waitFor();assert.equal(await page.locator('#ocr-enabled').isDisabled(),true);assert.equal(await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).isEnabled(),true);await page.keyboard.press('Escape');await page.unroute('**/api/capabilities',missingDestination);

  await (await reveal(page.getByRole('button',{name:'出力設定',exact:true,includeHidden:true}))).click();await page.waitForFunction(()=>document.querySelector('#ocr-model')?.value==='0');
  assert.equal(await page.locator('#ocr-enabled').isChecked(),false);assert.equal(await page.locator('#ocr-model').isDisabled(),true);
  const ocrBox=await page.getByRole('dialog').boundingBox();await page.locator('#ocr-enabled').check();const expandedOCR=await page.getByRole('dialog').boundingBox();assert.equal(expandedOCR.width,ocrBox.width);assert(expandedOCR.height>ocrBox.height,'explicit OCR enable expands the compact plain-PDF dialog');
  assert.equal(await page.locator('#ocr-quality').inputValue(),'standard');assert.equal(await page.locator('.ocr-languages input:checked').count(),2);
  for(const input of await page.locator('.ocr-languages input').all())await input.uncheck();assert.equal(await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).isDisabled(),true);
  await page.locator('.ocr-languages input[value=en]').check();await page.selectOption('#ocr-model','1');await page.getByText('OCRのために画像をFamily OCR serverへ送信します。',{exact:true}).waitFor();assert.equal(await page.locator('#ocr-model').getAttribute('aria-describedby'),'ocr-processing-notice');
  assert.equal(await page.locator('#ocr-quality').inputValue(),'');assert.equal(await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).isDisabled(),true);await page.selectOption('#ocr-quality','accurate');
  assert.equal(await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).isEnabled(),true);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:844});const bounds=await page.getByRole('dialog').boundingBox();assert.ok(bounds.x>=0&&bounds.x+bounds.width<=width,'OCR dialog exceeds viewport');assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)}
  await page.setViewportSize({width:390,height:844});
  await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-ocr-mobile.png'),fullPage:true});
  await page.keyboard.press('Escape');await page.selectOption('#locale','en');await page.locator('h1[data-document-title]').waitFor();await (await reveal(page.getByRole('button',{name:'Output settings',exact:true,includeHidden:true}))).click();await page.waitForFunction(()=>document.querySelector('#ocr-model')?.value==='0');await page.locator('#ocr-enabled').check();await page.selectOption('#ocr-model','1');await page.getByText('Images will be sent to Family OCR server for OCR.',{exact:true}).waitFor();await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-ocr-external-en.png'),fullPage:true});
  await page.keyboard.press('Escape');await page.selectOption('#locale','ja');await page.locator('h1[data-document-title]').waitFor();await (await reveal(page.getByRole('button',{name:'出力設定',exact:true,includeHidden:true}))).click();await page.waitForFunction(()=>document.querySelector('#ocr-model')?.value==='0');await page.locator('#ocr-enabled').check();await page.selectOption('#ocr-model','1');for(const input of await page.locator('.ocr-languages input').all())await input.uncheck();await page.locator('.ocr-languages input[value=en]').check();await page.selectOption('#ocr-quality','accurate');

  const commands=[];await page.route('**/api/documents/document_browser_01/render',async route=>{commands.push({key:route.request().headers()['idempotency-key'],body:route.request().postDataJSON()});if(commands.length===1)await route.fulfill({status:503,contentType:'application/json',body:'{"code":"provider_unavailable"}'});else await route.continue()});
  await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).click();await page.getByText('操作を完了できませんでした。再度お試しください。',{exact:true}).waitFor();assert.equal(await page.locator('#ocr-enabled').isChecked(),true);assert.equal(await page.locator('#ocr-quality').inputValue(),'accurate');
  await page.getByRole('dialog').getByRole('button',{name:'PDFを保存',exact:true}).click();await page.getByText('保存の準備中…',{exact:true}).waitFor();assert.equal(commands.length,2);assert.equal(commands[0].key,commands[1].key);
  assert.deepEqual(commands[1].body.ocr,{enabled:true,provider_id:'remote',model_id:'remote-model',languages:['en'],quality_preset:'accurate'});
  const frozen=await (await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/__fixture/latest-ocr')).json();assert.equal(frozen.ocr.model_version,'browser-model-v2');assert.equal(frozen.ocr.quality_preset,'accurate');
  const other=await browser.newContext({ignoreHTTPSErrors:true});const second=await other.newPage();await second.goto(process.env.SCANNER_TEST_ORIGIN);await second.getByRole('button',{name:'Start session',exact:true}).click();await second.getByRole('heading',{name:'Home',exact:true}).waitFor();await second.getByRole('button',{name:'History',exact:true}).click();await second.getByText('No documents',{exact:true}).waitFor();
  await page.getByRole('button',{name:'利用を終了',exact:true}).click();await page.getByRole('button',{name:'利用を開始',exact:true}).waitFor();
  assert.equal(await page.locator('footer#app-version').innerText(),runningVersion,'runtime version remains visible after ending the session');
  assert.deepEqual(errors,[]);assert.deepEqual(await page.evaluate(()=>window.__csp||[]),[]);
  console.log(JSON.stringify({passed:true,browser:browser.version(),checks:['external OCR requires a public destination while ordinary PDF remains available','English/Japanese external OCR destination disclosure and associated model control','embedded assets and real guest session','real history and private PNG preview','single overview, always-visible labeled icons and revision-safe rotation/reorder/delete APIs','server-derived active-job discovery from document/history, blocked edits, polling/focus, cancellation/reload and terminal stop','injected job state changes preserve action/header focus and restore removed-control focus','page identity/action focus survives reorder, delete, rejected edit and rotation','delayed committed edits preserve header focus, locale and later navigation','overview preserves page identity and action focus across reorder, deletion and rotation','real render HTTP to compiled worker to browser PDF download','output dialog outside/Escape close, stable layout, lossless PDF and real PNG download','OCR unavailable/plain independence, defaults, languages, explicit quality, external notice and stable retry admission','Japanese/English and neutral theme persistence','320/390/768/1440 layout','two-browser owner isolation','confirmed document/history deletion, same-key lost-response recovery, stale revision reload and real worker file cleanup','guest end','no JavaScript/CSP errors']}));
  await other.close();await context.close();
 }catch(error){console.error(JSON.stringify(await debug()));throw error}finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
