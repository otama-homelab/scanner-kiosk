const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const checks=[],errors=[];let mutations=0;
 try{
  for(const locale of ['en','ja'])for(const width of [320,1440])for(const view of ['session','home','history']){
   const context=await browser.newContext({viewport:{width,height:900}});
   await context.addInitScript(value=>localStorage.setItem('scanner.locale',value),locale);
   const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/api/**',route=>{
    if(route.request().method()!=='GET')mutations++;
    const path=new URL(route.request().url()).pathname;
    const body=path==='/api/session'?{authenticated:view!=='session',principal_kind:'guest',authenticate_type:'guest'}:path==='/api/scanners'?{scanners:[]}:{items:[],next_cursor:null};
    return route.fulfill({headers:{date:new Date().toUTCString()},json:body});
   });
   await page.goto(process.env.SCANNER_TEST_ORIGIN+(view==='history'?'#/history':''));
   await page.locator('main[aria-busy=false] h1').waitFor().catch(async error=>{console.error(JSON.stringify({body:(await page.locator('body').innerText()).slice(0,1500),errors}));throw error});
   const url=page.url();await page.keyboard.press('Tab');
   assert.equal(await page.evaluate(()=>document.activeElement?.id),'skip-link','first Tab must bypass repeated header controls');
   assert.equal(await page.locator('#skip-link').textContent(),locale==='en'?'Skip to content':'本文へ移動');
   const box=await page.locator('#skip-link').boundingBox();assert.ok(box&&box.x>=0&&box.y>=0&&box.x+box.width<=width,'focused skip link must be on screen');
   await page.keyboard.press('Enter');
   assert.equal(await page.evaluate(()=>document.activeElement?.id),'main');
   assert.equal(page.url(),url,'skip activation must preserve the current route');
   if(view==='session'){
    await page.keyboard.press('Tab');
    assert.equal(await page.evaluate(()=>document.querySelector('main').contains(document.activeElement)),true,'next Tab must continue in content');
   }
   await page.locator('#locale').selectOption(locale==='en'?'ja':'en');
   await page.locator('main[aria-busy=false] h1').waitFor();
   assert.equal(await page.locator('#skip-link').textContent(),locale==='en'?'本文へ移動':'Skip to content');
   assert.equal(page.url(),url);
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
   checks.push({locale,width,view});await context.close();
  }
  assert.deepEqual(errors,[]);assert.equal(mutations,0);
  console.log(JSON.stringify({passed:true,checks,mutations}));
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
