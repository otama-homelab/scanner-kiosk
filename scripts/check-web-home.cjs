const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const errors=[],checks=[];const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 let authenticated=true,auth='oidc',reads=0,hold=false,release,entered,fail=false,resetRequired=false,resetStatus=202,resetCalls=0,sessionReads=0;
 const capture={size:'a4',color:'color',dpi:300};let scanners=[];
 try{
  await page.route('**/api/**',async route=>{
   const path=new URL(route.request().url()).pathname;
   assert.equal(route.request().method(),'GET','viewing and polling must never mutate');
   if(path==='/api/session'){sessionReads++;return route.fulfill({headers:{date:new Date().toUTCString()},json:{authenticated,authenticate_type:auth,principal_kind:auth,reset_required:resetRequired}})}
   if(path==='/api/scanners'){
    reads++;if(hold){entered?.();await new Promise(resolve=>release=resolve)}
    if(fail)return route.fulfill({status:503,json:{code:'unavailable'}});
    return route.fulfill({json:{scanners}});
   }
   return route.fulfill({json:{items:[],next_cursor:null}});
  });
  await page.route('**/auth/session/reset',route=>{assert.equal(route.request().method(),'POST');assert.match(route.request().headers()['idempotency-key'],/^rq1_/);resetCalls++;if(resetStatus===202)resetRequired=false;if(resetStatus===409){resetRequired=false;authenticated=true}return route.fulfill({status:resetStatus,headers:{date:new Date().toUTCString()},body:resetStatus===202?'':JSON.stringify({code:resetStatus===409?'conflict':'session_unavailable'})})});
  await page.clock.install();await page.goto(process.env.SCANNER_TEST_ORIGIN);
  const refresh=page.getByRole('button',{name:'Try again',exact:true});
  await page.waitForFunction(()=>document.querySelector('.capture-state')?.textContent==='Not ready');
  await refresh.evaluate(button=>{window.__refreshDisabled=[];window.__refreshObserver=new MutationObserver(records=>{for(const record of records)window.__refreshDisabled.push(record.oldValue)});window.__refreshObserver.observe(button,{attributes:true,attributeFilter:['disabled'],attributeOldValue:true})});
  hold=true;let waiting=new Promise(resolve=>entered=resolve);await page.clock.fastForward(5000);await waiting;
  assert.equal(await refresh.isDisabled(),false,'background polling must not flash the refresh button disabled');
  const refreshed=page.waitForResponse('**/api/scanners');release();hold=false;await refreshed;
  assert.deepEqual(await page.evaluate(()=>window.__refreshDisabled),[],'background polling must keep the refresh button style stable');
  await page.evaluate(()=>window.__refreshObserver.disconnect());checks.push('Empty scanner background polling preserves button state');
  scanners=[{id:'scanner_home_00001',name:'Office',state:'idle',capture_options:[capture],default_capture:capture}];
  await page.clock.fastForward(5000);const scan=page.getByRole('button',{name:'Scan',exact:true});await scan.waitFor();await page.waitForFunction(()=>!document.querySelector('.capture-scan-start')?.disabled);
  assert.equal(await refresh.isVisible(),false,'healthy scanner has no retry control');scanners=scanners.map(device=>({...device,state:'unknown'}));await page.clock.fastForward(5000);await refresh.waitFor();
  hold=true;waiting=new Promise(resolve=>entered=resolve);await refresh.click();await waiting;assert(await refresh.isDisabled(),'explicit refresh shows pending state');
  const before=reads;await refresh.evaluate(button=>button.click());assert.equal(reads,before,'duplicate refresh must not start another request');
  fail=true;release();hold=false;await page.getByText('Scanning unavailable',{exact:true}).waitFor();assert(await scan.isDisabled(),'failed refresh must fail closed');
  hold=true;waiting=new Promise(resolve=>entered=resolve);await page.clock.fastForward(5000);await waiting;assert(await refresh.isVisible()&&await refresh.isEnabled(),'unavailable background recovery keeps retry visible and enabled');const failedPoll=page.waitForResponse('**/api/scanners');release();hold=false;await failedPoll;
  fail=false;scanners=scanners.map(device=>({...device,state:'idle'}));await refresh.click();await page.waitForFunction(()=>!document.querySelector('.capture-scan-start')?.disabled);checks.push('Explicit refresh is bounded, failure blocks scan and successful retry recovers');
  scanners=[{...scanners[0],id:'scanner_hotplug_0001',name:'New scanner'}];await page.clock.fastForward(5000);
  await page.getByRole('button',{name:'Select scanner',exact:true}).waitFor();assert(await scan.isDisabled(),'a disappeared selection must never silently move to different hardware');
  await page.getByRole('button',{name:'Select scanner',exact:true}).click();await page.getByRole('button',{name:/New scanner/}).click();assert(await scan.isEnabled());
  scanners.push({...scanners[0],id:'scanner_hotplug_0002',name:'Second scanner'});await page.clock.fastForward(5000);
  await page.getByRole('button',{name:'Select scanner',exact:true}).waitFor();assert(await scan.isEnabled(),'new devices preserve the current selection');
  checks.push('Hotplug preserves selection, allows selecting one remaining device and never silently redirects acquisition');
  for(const locale of ['en','ja'])for(const width of [320,768,1440]){
   await page.setViewportSize({width,height:900});await page.selectOption('#locale',locale);await page.locator('main[aria-busy=false] h1').waitFor();
   const entry=page.getByRole('button',{name:locale==='en'?'Import PDF':'PDFを取り込む',exact:true});
   const box=await entry.boundingBox(),panels=await page.locator('.capture-panels').boundingBox();
   assert(box.y+box.height<=panels.y&&await entry.evaluate(button=>button.parentElement.contains(document.querySelector('main h1'))),'Import PDF belongs with the home heading, before scan panels');
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   if(process.env.SCANNER_TEST_SCREENSHOTS)await page.screenshot({path:`${process.env.SCANNER_TEST_SCREENSHOTS}/home-${locale}-${width}.png`,fullPage:true});
  }
  checks.push('Bilingual home action grouped with heading at mobile, tablet and desktop widths');
  authenticated=false;
  for(const locale of ['en','ja'])for(const width of [320,768,1440])for(const theme of ['light','dark']){
   await page.setViewportSize({width,height:900});await page.evaluate(({locale,theme})=>{localStorage.setItem('scanner.locale',locale);localStorage.setItem('scanner.theme',theme)},{locale,theme});await page.reload();
   const card=page.locator('.session-entry');await card.waitFor().catch(async error=>{console.error(JSON.stringify({body:await page.locator('main').innerText(),errors}));throw error});const box=await card.boundingBox();
   assert(box.width<=480&&box.x>=0&&box.x+box.width<=width,'sign-in has a readable centered width');
   assert.equal(await page.locator('main h1').textContent(),locale==='en'?'Sign in':'ログイン');
   assert(await page.getByRole('button',{name:locale==='en'?'Sign in':'ログイン',exact:true}).isEnabled());
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   if(process.env.SCANNER_TEST_SCREENSHOTS)await page.screenshot({path:`${process.env.SCANNER_TEST_SCREENSHOTS}/signin-${locale}-${width}-${theme}.png`,fullPage:true});
  }
  checks.push('Bilingual OIDC entry is centered, responsive and usable in both themes');
  auth='guest';await page.reload();await page.getByRole('button',{name:'利用を開始',exact:true}).waitFor();assert.equal(await page.locator('main h1').textContent(),'セッション');
  auth='oidc';authenticated=false;resetRequired=true;const beforeRecovery=sessionReads;await page.reload();await page.getByRole('heading',{name:'ログイン',exact:true}).waitFor();assert.equal(resetCalls,1);assert.equal(sessionReads,beforeRecovery+2);
  authenticated=false;resetRequired=true;resetStatus=409;await page.reload();await page.getByRole('heading',{name:'ホーム',exact:true}).waitFor();assert.equal(resetCalls,2);
  authenticated=false;resetRequired=true;resetStatus=503;await page.reload();await page.getByText('操作を完了できませんでした。再度お試しください。',{exact:true}).waitFor();assert.equal(resetCalls,3);await page.clock.fastForward(1000);assert.equal(resetCalls,3,'failed recovery must not retry or replay a login');
  checks.push('Stale cookie recovery is automatic and bounded; concurrent valid login survives, storage failure never loops');
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:true,checks}));
 }finally{release?.();await context.close();await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
