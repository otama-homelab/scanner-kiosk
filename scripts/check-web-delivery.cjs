const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const http=require('node:http');
const path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
test('TestDeliveryScenarioAdmission',()=>{
 const result=require('node:child_process').spawnSync('python3',['scripts/check-web.py','--help'],{encoding:'utf8'});
 assert.equal(result.status,0);assert.match(result.stdout,/structure-suggestion,delivery/,'real runner must admit the delivery lifecycle scenario');
});
if(process.env.SCANNER_TEST_ORIGIN)test('TestDeliveryRealLifecycle',async()=>{
 let browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
 let context=await browser.newContext({ignoreHTTPSErrors:true}),page=await context.newPage();const origin=process.env.SCANNER_TEST_ORIGIN,checks=[];
 const fixture=async(action,value)=>{const response=await context.request.post(origin+'/__fixture/delivery',{data:{action,value}});assert.equal(response.status(),200);return response.json()};
 const download=async(name)=>{const response=await context.request.get(origin+'/api/documents/document_delivery_'+name+'/download');assert.equal(response.status(),200);const digest=require('node:crypto').createHash('sha256').update(await response.body()).digest('hex');assert.equal(digest,(await fixture('observe')).pdf_sha256[name]);checks.push('unchanged Save '+name);return digest};
 const open=async(name)=>{await page.goto(origin+'/#/document/document_delivery_'+name);await page.locator('h1[data-document-title]').waitFor();await page.waitForTimeout(300)};
 const send=()=>page.getByRole('button',{name:'Send to Paperless',exact:true});
 const confirm=async()=>{await send().click();await page.getByRole('button',{name:'Confirm send',exact:true}).click()};
 const waitState=state=>page.locator('[data-delivery-state="'+state+'"]').waitFor();
 const task='12345678-1234-4234-8234-123456789abc';
 try{
 await page.goto(origin);await page.getByRole('heading',{name:'Home',exact:true}).waitFor();await fixture('seed');await open('happy');assert.equal(await page.locator('[data-delivery]').count(),0);await download('happy');checks.push('disabled delivery DOM absent');
 await fixture('setting','unconfigured');await page.reload();await page.getByText('Paperless is not configured.',{exact:true}).waitFor();assert.equal(await send().count(),0);await download('happy');checks.push('unconfigured no send');
 await fixture('setting','configured');await page.reload();await page.getByText('Connection not checked',{exact:true}).waitFor();await page.getByRole('button',{name:'Check connection',exact:true}).click();await page.getByText(/^Connection check passed/).waitFor();assert.equal((await fixture('observe')).uploads,0);checks.push('health GET only upload zero');
 await send().focus();await page.keyboard.press('Enter');await page.getByRole('dialog').waitFor();assert.match(await page.getByRole('dialog').innerText(),/Household archive/);await page.keyboard.press('Escape');await page.waitForFunction(()=>!document.querySelector('dialog[open]'));assert(await send().evaluate(e=>e===document.activeElement));assert.equal((await fixture('observe')).uploads,0);
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});await send().click();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'dialog overflow '+width);await page.screenshot({path:'/tmp/scanner48-delivery-'+width+'.png'});await page.keyboard.press('Escape');checks.push('width/keyboard/focus '+width)}
 await confirm();await waitState('queued');assert.equal((await fixture('observe')).uploads,0);await fixture('start-worker');await waitState('accepted');await download('happy');await fixture('mode','success');await waitState('confirmed');assert.equal((await fixture('observe')).uploads,1);assert.equal(await send().count(),0);checks.push('real queued accepted checksum confirmed POST1');await fixture('stop-worker');
 // Cancellation before the worker starts must not create a remote attempt.
 await open('cancel');await confirm();await waitState('queued');await page.getByRole('button',{name:'Stop delivery locally',exact:true}).click();await waitState('cancelled');assert.equal((await fixture('observe')).uploads,1);await download('cancel');checks.push('queued cancellation POST0');
 await fixture('mode','loss');await open('loss');await confirm();await waitState('queued');await fixture('start-worker');await waitState('unknown');assert.equal((await fixture('observe')).uploads,2);await fixture('restart');const storageState=await context.storageState();await browser.close();
 browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});context=await browser.newContext({ignoreHTTPSErrors:true,storageState});page=await context.newPage();await open('loss');await waitState('unknown');assert.equal(await send().count(),0);await download('loss');checks.push('real worker API and browser restart preserve unknown POST1');
 for(const mode of ['missing','marker','checksum','schema10']){
  await fixture('mode',mode);await page.getByLabel('Exact Paperless task UUID').fill(task);const response=page.waitForResponse(r=>r.url().endsWith('/reconcile'));await page.getByRole('button',{name:'Reconcile task',exact:true}).click();assert.equal((await response).status(),200);await waitState('unknown');assert.equal(await send().count(),0);assert.equal((await fixture('observe')).uploads,2);await download('loss');checks.push('unknown recovery '+mode+' no upload');
 }
 const replay=await page.evaluate(async()=>{const results=[];for(let i=0;i<2;i++){const r=await fetch('/api/documents/document_delivery_loss/deliveries',{method:'POST',headers:{'Content-Type':'application/json','If-Match':'"1"','Idempotency-Key':'rq1_'+String(Date.now()).padStart(13,'0')+'_'+crypto.randomUUID().replaceAll('-','')},body:JSON.stringify({destination:'paperless',confirmed:true})});results.push({status:r.status,value:await r.json()})}return results});assert(replay.every(r=>r.status===202&&r.value.state==='unknown'));assert.equal(replay[0].value.id,replay[1].value.id);assert.equal((await fixture('observe')).uploads,2);checks.push('new keys replay same unknown ledger without upload');
 await fixture('mode','success');await page.getByLabel('Exact Paperless task UUID').fill(task);await page.getByRole('button',{name:'Reconcile task',exact:true}).click();await waitState('confirmed');assert.equal((await fixture('observe')).uploads,2);checks.push('exact task GET and checksum confirm without resend');
 await fixture('mode','slow');await open('slow');await confirm();await waitState('queued');await fixture('start-worker');const deadline=Date.now()+10000;while((await fixture('observe')).uploads<3){assert(Date.now()<deadline);await page.waitForTimeout(50)}
 await download('slow');await page.getByRole('button',{name:'Stop delivery locally',exact:true}).click();await waitState('unknown');await page.getByText('Remote processing may continue. Stopping locally does not cancel Paperless processing.',{exact:true}).waitFor();assert.equal((await fixture('observe')).uploads,3);checks.push('slow external send preserves normal download and honest local cancel');await fixture('stop-worker');
 fs.writeFileSync(process.env.SCANNER_TEST_RECEIPT,JSON.stringify({passed:true,real_backend:true,checks,upload_counts:{happy:1,loss_recovery:1,queued_cancel:0,slow:1}},null,2)+'\n');
 }finally{await browser.close()}
});
test('TestDeliveryExplicitConfirmationAndRecovery',async()=>{
 const root=path.resolve(__dirname,'../internal/webui/dist');
 const server=http.createServer((req,res)=>{const file=path.join(root,req.url.split('?')[0]==='/'?'index.html':req.url.split('?')[0]);try{res.setHeader('Content-Type',file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(file))}catch{res.writeHead(404);res.end()}});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
 try{
 const page=await browser.newPage();let enabled=true,configured=true,health='unknown',state=null,posts=0,loss=false,destination='Household archive',sourceDocument='document_delivery_browser01',rejectCancel=false;
 const doc={id:'document_delivery_browser01',purpose:'document',state:'ready',revision:1,created_at:new Date().toISOString(),expires_at:new Date(Date.now()+3600000).toISOString(),pages:[],output:{revision:1,media_type:'application/pdf'},active_job_id:null};
 const projection=()=>({id:'delivery_browser_0001',document_id:sourceDocument,revision:1,destination:'paperless',state,remote_processing_may_continue:state==='unknown'});
 await page.route('**/api/**',async route=>{
  const req=route.request(),url=new URL(req.url()),method=req.method();
  if(url.pathname==='/api/session')return route.fulfill({headers:{date:new Date().toUTCString()},json:{authenticated:true,authenticate_type:'none',principal_kind:'guest'}});
  if(url.pathname==='/api/capabilities')return route.fulfill({json:enabled?{paperless:{enabled,configured,confirmation_required:true,processing_destination:destination,health:{status:health}}}:{}});
  if(url.pathname.endsWith('/health')){health='healthy';return route.fulfill({json:{status:health,checked_at:new Date().toISOString()}})}
  if(url.pathname==='/api/documents/'+doc.id)return route.fulfill({json:doc});
  if(url.pathname.endsWith('/current'))return state?route.fulfill({json:projection()}):route.fulfill({status:404,json:{code:'not_found'}});
  if(url.pathname.endsWith('/deliveries')&&method==='POST'){posts++;assert.deepEqual(req.postDataJSON(),{destination:'paperless',confirmed:true});assert.equal(req.headers()['if-match'],'"1"');assert.match(req.headers()['idempotency-key'],/^rq1_/);state=loss?'unknown':'queued';if(loss){await new Promise(resolve=>setTimeout(resolve,400));return route.abort()}return route.fulfill({status:202,json:projection()})}
  if(url.pathname.endsWith('/reconcile')){assert.equal(req.postDataJSON().task_id,'12345678-1234-4234-8234-123456789abc');state='confirmed';return route.fulfill({json:projection()})}
  if(url.pathname.endsWith('/cancel')){if(rejectCancel)return route.fulfill({status:503,json:{message:'<script>provider private raw text</script>'}});state='cancelled';return route.fulfill({json:projection()})}
  return route.fulfill({json:projection()});
 });
 const load=async()=>{await page.goto('http://127.0.0.1:'+server.address().port+'/#/document/'+doc.id);await page.locator('h1').waitFor({timeout:3000}).catch(async err=>{console.error(await page.locator('body').innerText());throw err});await page.waitForTimeout(100)};
 const send=()=>page.getByRole('button',{name:'Send to Paperless',exact:true});
 await load();assert.equal(await send().count(),1,'reviewed saved PDF exposes explicit secondary delivery action');
 assert(await page.getByRole('link',{name:'Save PDF',exact:true}).isVisible());
 await send().focus();await page.keyboard.press('Enter');await page.getByRole('dialog').waitFor();assert.match(await page.getByRole('dialog').innerText(),/Household archive/);
 await page.keyboard.press('Escape');await page.waitForFunction(()=>!document.querySelector('dialog[open]'));assert.equal(posts,0);assert(await send().evaluate(e=>e===document.activeElement));
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});await send().click();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.keyboard.press('Escape')}
 loss=true;await send().click();await page.getByRole('button',{name:'Confirm send',exact:true}).click();await page.keyboard.press('Escape');assert(await page.getByRole('dialog').isVisible());assert(await page.getByRole('button',{name:'Confirm send',exact:true}).isDisabled());await page.getByText('Delivery outcome unknown',{exact:true}).waitFor();assert.equal(await send().count(),0);assert.equal(posts,1);
 await page.reload();await page.getByText('Delivery outcome unknown',{exact:true}).waitFor();assert.equal(await send().count(),0);
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'unknown form overflow '+width)}
 await page.getByLabel('Exact Paperless task UUID').fill('12345678-1234-4234-8234-123456789abc');await page.getByRole('button',{name:'Reconcile task',exact:true}).click();await page.getByText('Delivery confirmed',{exact:true}).waitFor();assert.equal(posts,1);
 for(const value of ['queued','attempted','accepted','failed','cancelled']){state=value;await page.reload();await page.locator('[data-delivery-state="'+value+'"]').waitFor();assert.equal(await send().count(),0)}
 state='queued';rejectCancel=true;await page.reload();await page.getByRole('button',{name:'Stop delivery locally',exact:true}).click();await page.getByText('Delivery status unavailable. Check status before sending.',{exact:true}).waitFor({timeout:2000});assert(!await page.locator('main').innerText().then(text=>text.includes('provider private raw text')));assert(await page.getByRole('link',{name:'Save PDF',exact:true}).isVisible());rejectCancel=false;
 sourceDocument='document_delivery_other01';state='confirmed';await page.reload();await page.getByText('Delivery confirmed',{exact:true}).waitFor({timeout:2000});assert.equal(await send().count(),0,'semantic content replay must show the existing status without resending');sourceDocument=doc.id;
 state=null;destination='Household'.repeat(50);await page.reload();await send().waitFor();for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'long destination panel overflow '+width);await send().click();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'long destination dialog overflow '+width);await page.keyboard.press('Escape')}
 health='unavailable';await page.reload();await page.getByText('Connection check failed. You can still save this PDF.',{exact:true}).waitFor();assert(await page.getByRole('link',{name:'Save PDF',exact:true}).isVisible());
 state=null;configured=false;await page.reload();await page.getByText('Paperless is not configured.',{exact:true}).waitFor();assert.equal(await send().count(),0);
 enabled=false;await page.reload();await page.locator('h1').waitFor();await page.waitForTimeout(100);assert.equal(await page.locator('[data-delivery]').count(),0);
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve))}
});
