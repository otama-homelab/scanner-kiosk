const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const http=require('node:http');
const path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
test('TestDeliveryExplicitConfirmationAndRecovery',async()=>{
 const root=path.resolve(__dirname,'../internal/webui/dist');
 const server=http.createServer((req,res)=>{const file=path.join(root,req.url.split('?')[0]==='/'?'index.html':req.url.split('?')[0]);try{res.setHeader('Content-Type',file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(file))}catch{res.writeHead(404);res.end()}});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
 try{
 const page=await browser.newPage();let enabled=true,configured=true,health='unknown',state=null,posts=0,loss=false;
 const doc={id:'document_delivery_browser01',purpose:'document',state:'ready',revision:1,created_at:new Date().toISOString(),expires_at:new Date(Date.now()+3600000).toISOString(),pages:[],output:{revision:1,media_type:'application/pdf'},active_job_id:null};
 const projection=()=>({id:'delivery_browser_0001',document_id:doc.id,revision:1,destination:'paperless',state,remote_processing_may_continue:state==='unknown'});
 await page.route('**/api/**',async route=>{
  const req=route.request(),url=new URL(req.url()),method=req.method();
  if(url.pathname==='/api/session')return route.fulfill({headers:{date:new Date().toUTCString()},json:{authenticated:true,authenticate_type:'none',principal_kind:'guest'}});
  if(url.pathname==='/api/capabilities')return route.fulfill({json:enabled?{paperless:{enabled,configured,confirmation_required:true,processing_destination:'Household archive',health:{status:health}}}:{}});
  if(url.pathname.endsWith('/health')){health='healthy';return route.fulfill({json:{status:health,checked_at:new Date().toISOString()}})}
  if(url.pathname==='/api/documents/'+doc.id)return route.fulfill({json:doc});
  if(url.pathname.endsWith('/current'))return state?route.fulfill({json:projection()}):route.fulfill({status:404,json:{code:'not_found'}});
  if(url.pathname.endsWith('/deliveries')&&method==='POST'){posts++;assert.deepEqual(req.postDataJSON(),{destination:'paperless',confirmed:true});assert.equal(req.headers()['if-match'],'"1"');assert.match(req.headers()['idempotency-key'],/^rq1_/);state=loss?'unknown':'queued';if(loss)return route.abort();return route.fulfill({status:202,json:projection()})}
  if(url.pathname.endsWith('/reconcile')){assert.equal(req.postDataJSON().task_id,'12345678-1234-4234-8234-123456789abc');state='confirmed';return route.fulfill({json:projection()})}
  if(url.pathname.endsWith('/cancel')){state='cancelled';return route.fulfill({json:projection()})}
  return route.fulfill({json:projection()});
 });
 const load=async()=>{await page.goto('http://127.0.0.1:'+server.address().port+'/#/document/'+doc.id);await page.locator('h1').waitFor({timeout:3000}).catch(async err=>{console.error(await page.locator('body').innerText());throw err});await page.waitForTimeout(100)};
 const send=()=>page.getByRole('button',{name:'Send to Paperless',exact:true});
 await load();assert.equal(await send().count(),1,'reviewed saved PDF exposes explicit secondary delivery action');
 assert(await page.getByRole('link',{name:'Save PDF',exact:true}).isVisible());
 await send().focus();await page.keyboard.press('Enter');await page.getByRole('dialog').waitFor();assert.match(await page.getByRole('dialog').innerText(),/Household archive/);
 await page.keyboard.press('Escape');await page.waitForFunction(()=>!document.querySelector('dialog[open]'));assert.equal(posts,0);assert(await send().evaluate(e=>e===document.activeElement));
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});await send().click();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.keyboard.press('Escape')}
 loss=true;await send().click();await page.getByRole('button',{name:'Confirm send',exact:true}).click();await page.getByText('Delivery outcome unknown',{exact:true}).waitFor();assert.equal(await send().count(),0);assert.equal(posts,1);
 await page.reload();await page.getByText('Delivery outcome unknown',{exact:true}).waitFor();assert.equal(await send().count(),0);
 await page.getByLabel('Exact Paperless task UUID').fill('12345678-1234-4234-8234-123456789abc');await page.getByRole('button',{name:'Reconcile task',exact:true}).click();await page.getByText('Delivery confirmed',{exact:true}).waitFor();assert.equal(posts,1);
 for(const value of ['queued','attempted','accepted','failed','cancelled']){state=value;await page.reload();await page.locator('[data-delivery-state="'+value+'"]').waitFor();assert.equal(await send().count(),0)}
 state=null;configured=false;await page.reload();await page.getByText('Paperless is not configured.',{exact:true}).waitFor();assert.equal(await send().count(),0);
 enabled=false;await page.reload();await page.locator('h1').waitFor();await page.waitForTimeout(100);assert.equal(await page.locator('[data-delivery]').count(),0);
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve))}
});
