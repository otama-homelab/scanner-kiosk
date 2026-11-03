const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const checks=[],errors=[];const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 const capture={size:'a4',color:'color',dpi:300};let devices=[{id:'scanner_capture_001',name:'Office',state:'idle',capture_options:[capture,{size:'a5',color:'gray',dpi:600}],default_capture:capture},{id:'scanner_capture_002',name:'Studio',state:'busy',capture_options:[capture],default_capture:capture}];
 const posts=[];let response='reject',release,entered;
 try{
  await page.route('**/api/scanners',route=>route.fulfill({json:{revision:'1'.repeat(64),scanners:devices,default_capture:capture,limits:{}}}));
  const job={id:'job_capture_browser_001',document_id:'document_capture_001',kind:'scan',state:'queued',input_revision:1,error_code:null};
  await page.route('**/api/jobs/job_capture_browser_001',route=>route.fulfill({json:job}));
  await page.route('**/api/documents',async route=>{
   if(route.request().method()!=='POST')return route.continue();posts.push({body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']});
   if(response==='pending'){entered();await new Promise(resolve=>release=resolve);return route.abort('aborted').catch(()=>{})}
   if(response==='unknown')return route.abort('failed');
   if(response==='expired')return route.fulfill({status:410,json:{code:'gone',message:'Gone'}});
   if(response==='reject')return route.fulfill({status:503,json:{code:'scanner_unavailable',message:'Unavailable'}});
   return route.fulfill({status:202,json:job});
  });
  await page.goto(process.env.SCANNER_TEST_ORIGIN);await page.getByRole('button',{name:'Start session',exact:true}).click();await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  const scan=page.getByRole('button',{name:'Scan',exact:true});assert(await scan.isDisabled());
  for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));assert(await page.getByRole('link',{name:'Scanner',exact:true}).isVisible(),'brand home link must keep its accessible name at every width')}
  await page.setViewportSize({width:320,height:900});
  assert(await page.getByRole('button',{name:'Document',exact:true}).evaluate(button=>{const range=document.createRange();range.selectNodeContents(button);return range.getClientRects().length===1}),'ordinary Document label must fit one line at 320px');
  checks.push('English responsive capture view; multiple devices require explicit selection');
  await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByLabel('Search scanners',{exact:true}).fill('stu');await page.getByRole('button',{name:/Studio/}).click();assert(await scan.isDisabled());
  await page.getByRole('button',{name:'Change scanner',exact:true}).click();await page.getByLabel('Search scanners',{exact:true}).fill('off');await page.getByRole('button',{name:/Office/}).click();await scan.waitFor();assert(!(await scan.isDisabled()));
  await page.getByRole('button',{name:'Change scanner',exact:true}).click();await page.mouse.click(4,4);await page.locator('.capture-dialog').waitFor({state:'detached'});
  checks.push('Search/select modality, busy refusal and outside dismissal');
  await page.getByRole('button',{name:'Scan settings',exact:true}).click();await page.getByLabel('Paper size',{exact:true}).selectOption('a5');assert.equal(await page.getByLabel('Color',{exact:true}).inputValue(),'gray');
  const height=await page.locator('.capture-dialog').evaluate(e=>e.getBoundingClientRect().height);await page.getByText('Advanced',{exact:true}).click();assert.equal(await page.getByLabel('DPI',{exact:true}).inputValue(),'600');assert.equal(await page.locator('.capture-dialog').evaluate(e=>e.getBoundingClientRect().height),height);await page.getByRole('button',{name:'Apply',exact:true}).click();
  checks.push('Verified coupled settings only; DPI under Advanced and fixed dialog height');
  await page.evaluate(()=>{window.__requestKeyDateNow=Date.now;Date.now=()=>0});
  await scan.click();await page.getByText('Scanning could not start. Check the scanner and try again.',{exact:true}).waitFor();await page.waitForFunction(()=>!Array.from(document.querySelectorAll('button')).find(e=>e.textContent==='Scan')?.disabled);assert.equal(posts.length,1);assert.deepEqual(posts[0].body,{scanner_id:'scanner_capture_001',purpose:'document',capture:{size:'a5',color:'gray',dpi:600}});
  response='success';await scan.click();await page.getByText('Queued',{exact:true}).waitFor();assert.equal(posts.length,2);assert.equal(posts[0].key,posts[1].key);assert.match(posts[0].key,/^rq1_[0-9]{13}_[0-9a-f]{32}$/);assert(Math.abs(Number(posts[0].key.slice(4,17))-Date.now())<30000);await page.evaluate(()=>{Date.now=window.__requestKeyDateNow;delete window.__requestKeyDateNow});
  checks.push('Explicit-only POST; known rejection can reuse the same intent key, acceptance opens server job');
  await page.getByRole('button',{name:'Home',exact:true}).click();await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByRole('button',{name:/Office/}).click();
  response='expired';await scan.click();await page.getByText('This request expired. Reload this view before choosing another action.',{exact:true}).waitFor();assert(await scan.isDisabled());const expiredPosts=posts.length;
  await page.getByRole('button',{name:'Reload view',exact:true}).click();await page.getByRole('heading',{name:'Home',exact:true}).waitFor();assert.equal(posts.length,expiredPosts);
  checks.push('Expired request blocks scan until explicit view reload; reload never silently repeats or refreshes a native intent');

  await page.getByRole('button',{name:'Home',exact:true}).click();await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByRole('button',{name:/Office/}).click();await page.getByRole('button',{name:'Photo',exact:true}).click();await page.getByRole('button',{name:'Scan settings',exact:true}).click();await page.getByLabel('Image format',{exact:true}).selectOption('png');await page.getByRole('button',{name:'Apply',exact:true}).click();
  response='unknown';await scan.click();await page.getByText('The scan may have started. Check History before scanning again.',{exact:true}).waitFor();assert(await scan.isDisabled());assert.equal(posts.at(-1).body.photo_format,'png');const count=posts.length;await page.waitForTimeout(250);assert.equal(posts.length,count);
  checks.push('PNG photo intent; unknown response blocks re-submission and offers History');
  await page.getByRole('button',{name:'Home',exact:true}).click();await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByRole('button',{name:/Office/}).click();
  let seen=new Promise(resolve=>entered=resolve);response='pending';await scan.click();await seen;await page.selectOption('#locale','ja');await page.getByRole('heading',{name:'ホーム',exact:true}).waitFor();release();await page.waitForTimeout(100);assert.equal(posts.length,count+1);assert.equal(await page.locator('html').getAttribute('lang'),'ja');
  checks.push('Locale/view disposal aborts pending capture without replay or late navigation');
  await page.selectOption('#locale','en');await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  let retryPurpose='document';
  await page.route('**/api/documents/document_capture_001',route=>route.fulfill({json:{id:'document_capture_001',revision:5,purpose:retryPurpose,photo_format:'png',state:'review',expires_at:new Date(Date.now()+3600000).toISOString(),pages:[],output:null}}));
  let next=null;await page.route('**/api/documents/document_capture_001/scans',route=>{next={body:route.request().postDataJSON(),revision:route.request().headers()['if-match']};return route.fulfill({status:202,json:job})});
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/document_capture_001/scan');await page.getByRole('heading',{name:'Scan next page',exact:true}).waitFor();await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByRole('button',{name:/Office/}).click();await page.getByRole('button',{name:'Scan next page',exact:true}).click();await page.getByText('Queued',{exact:true}).waitFor();assert.equal(next.revision,'"5"');assert.deepEqual(next.body,{scanner_id:'scanner_capture_001',capture});
  checks.push('Next-page command uses server document revision and excludes original type/output fields');
  retryPurpose='photo';await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/document_capture_001/scan');await page.getByRole('heading',{name:'Scan next page',exact:true}).waitFor();await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByRole('button',{name:/Office/}).click();await page.getByRole('button',{name:'Scan settings',exact:true}).click();assert.equal(await page.getByLabel('Image format',{exact:true}).count(),0);await page.getByRole('button',{name:'Close',exact:true}).click();await page.locator('.capture-summary').getByText('PNG',{exact:true}).waitFor();checks.push('Empty photo retry retains the server PNG intent and does not offer an ignored override');
  devices=[{...devices[0],state:'unknown',capture_options:[],default_capture:null}];await page.getByRole('button',{name:'Home',exact:true}).click();await page.getByText('Not ready',{exact:true}).waitFor();assert(await scan.isDisabled());assert.equal(await page.getByRole('button',{name:'Change scanner',exact:true,includeHidden:true}).count(),1);assert(!(await page.getByRole('button',{name:'Change scanner',exact:true}).isVisible()));
  await page.setViewportSize({width:390,height:844});await page.evaluate(()=>scrollTo(0,0));const primaryBox=await scan.boundingBox();assert(primaryBox.y+primaryBox.height<=844,'primary scan action must fit the mobile viewport');await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT,fullPage:true});checks.push('Single device skips selection and missing readiness cannot enable Scan');
  devices=[{...devices[0],state:'idle',capture_options:[capture],default_capture:capture}];await page.getByRole('button',{name:'Try again',exact:true}).click();await page.waitForFunction(()=>!Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='Scan')?.disabled);assert.equal(posts.length,count+1);checks.push('Initial metadata arrival selects verified defaults without posting a scan');
  const namePosts=posts.length;
  for(const [name,accepted] of [['a'.repeat(256),true],['a'.repeat(257),false],['',false],['書'.repeat(256),true],['書'.repeat(257),false],['📚'.repeat(256),true],['📚'.repeat(257),false]]){
   devices=[{...devices[0],name,state:'idle',capture_options:[capture],default_capture:capture}];
   await page.getByRole('button',{name:'Home',exact:true}).click();
   await page.waitForFunction(({name,accepted})=>{
    const identity=document.querySelector('.capture-identity'),scan=document.querySelector('.capture-settings>.primary'),state=document.querySelector('.capture-state');
    return accepted?identity?.textContent===name&&scan?.disabled===false:scan?.disabled===true&&state?.textContent==='Scanning unavailable';
   },{name,accepted});
  }
  assert.equal(posts.length,namePosts);checks.push('Scanner names preserve 1–256 Unicode code points; empty and over-limit inventories fail closed without scanning');
  assert.deepEqual(errors,[]);process.stdout.write(JSON.stringify({passed:true,checks}));
 }finally{release?.();await context.close();await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
