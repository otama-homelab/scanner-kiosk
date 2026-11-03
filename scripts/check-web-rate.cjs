const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);

// Exercise the embedded UI with explicit API faults, not real rate exhaustion.
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true,locale:'en-US'});
  const doc={id:'document_rate_001',revision:1,purpose:'document',state:'review',page_count:0,pages:[],expires_at:'2099-01-01T00:00:00Z'};
  let limited=null,reads=0,writes=0;
  await context.route('**/api/**',async route=>{
   const url=new URL(route.request().url());const headers={Date:new Date().toUTCString()};
   if(url.pathname==='/api/documents/'+doc.id){
    reads++;
    if(limited){if(limited.header!==null)headers['Retry-After']=limited.header;await route.fulfill({status:429,json:{code:'rate_limited'},headers});return}
   }
   const value=url.pathname==='/api/session'?{authenticated:true,authenticate_type:'guest',principal_kind:'guest'}:url.pathname==='/api/documents'?{items:[doc],next_cursor:null}:url.pathname==='/api/documents/'+doc.id?doc:null;
   await route.fulfill({status:value?200:404,json:value,headers});
  });
  await context.route('**/auth/**',async route=>{writes++;await route.fulfill({status:429,json:{code:'rate_limited'},headers:{Date:new Date().toUTCString(),'Retry-After':'1'}})});
  const page=await context.newPage();await page.setViewportSize({width:390,height:844});
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/history');
  const checks=[];
  for(const [header,seconds,locale] of [['1',1,'en'],['1.2',2,'en'],['bad',2,'en'],[null,2,'en'],['Infinity',2,'en'],['999',60,'en'],['3',3,'ja']]){
   await page.locator('#history').click();await page.selectOption('#locale',locale);
   const open=page.getByRole('button',{name:locale==='ja'?'開く':'Open',exact:true});await open.waitFor();
   limited={header};reads=0;await open.click();
   const message=locale==='ja'?`操作が集中しています。${seconds}秒ほど待ってから、もう一度お試しください。`:`Too many requests. Wait ${seconds}s, then try again.`;
   await page.getByText(message,{exact:true}).waitFor();assert.equal(reads,1);assert.ok(page.url().endsWith('/document/'+doc.id));
   if(header==='1'){await page.waitForTimeout(1200);assert.equal(reads,1,'route must wait for explicit retry')}
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'rate notice overflows mobile');
   limited=null;await page.getByRole('button',{name:locale==='ja'?'再読み込み':'Try again',exact:true}).click();
   await page.getByRole('heading',{name:locale==='ja'?'書類':'Document',exact:true}).waitFor();assert.equal(reads,2);
   checks.push({header,seconds,locale});
  }
  await page.selectOption('#locale','en');await page.getByRole('heading',{name:'Document',exact:true}).waitFor();
  await page.locator('#logout').click();await page.getByText('Too many requests. Wait 1s, then try again.',{exact:true}).waitFor();
  await page.waitForTimeout(1200);assert.equal(writes,1,'mutation must not be automatically repeated');checks.push('mutation is not automatically resent');
  console.log(JSON.stringify({passed:true,checks,scope:'Embedded frontend with injected API responses; no server limiter or native acceptance'}));
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
