const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();const responses=[];page.on('response',async r=>{const path=new URL(r.url()).pathname;if(path.startsWith('/api/')||path.startsWith('/auth/'))responses.push({path,status:r.status(),error:r.status()>=400?await r.text():undefined})});const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(process.env.SCANNER_TEST_ORIGIN);await page.getByRole('heading',{name:'Home',exact:true}).waitFor().catch(async e=>{console.error(JSON.stringify({body:await page.locator('body').innerText(),errors}));throw e});
  assert.equal(await page.getByRole('button',{name:/^(Start session|Sign in)$/}).count(),0);
  const pdfPage=await context.newPage();await pdfPage.setContent('<p>Anonymous import fixture</p>');const pdf=await pdfPage.pdf();await pdfPage.close();
  const choosing=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Import PDF',exact:true}).click();
  await (await choosing).setFiles({name:'anonymous.pdf',mimeType:'application/pdf',buffer:pdf});
  await page.locator('h1[data-document-title]').waitFor().catch(async e=>{console.error(JSON.stringify({stage:'import',responses,body:await page.locator('body').innerText(),errors}));throw e});const documentURL=page.url();const id=documentURL.split('/').at(-1);
  const rotated=page.waitForResponse(r=>r.request().method()==='PATCH'&&r.url().includes('/pages/'));
  await page.getByRole('button',{name:'Rotate',exact:true}).click();assert.equal((await rotated).status(),200);
  assert.equal((await (await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/documents/'+id)).json()).pages[0].rotation,90);
  await page.reload();await page.locator('h1[data-document-title]').waitFor();
  const download=page.waitForEvent('download');await page.getByRole('button',{name:'Save PDF',exact:true}).click();const saved=await download;
  assert.equal(await saved.failure(),null);
  const other=await browser.newContext({ignoreHTTPSErrors:true});const second=await other.newPage();await second.goto(process.env.SCANNER_TEST_ORIGIN);await second.getByRole('heading',{name:'Home',exact:true}).waitFor();
  assert.equal((await second.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/documents/'+id)).status(),404);
  await second.locator('#history').click();await second.getByText('No documents',{exact:true}).waitFor();
  await page.locator('#logout').click();await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  assert.equal((await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/documents/'+id)).status(),404);
  await second.reload();await second.getByRole('heading',{name:'History',exact:true}).waitFor();
  assert.deepEqual(errors,[]);await other.close();await context.close();
  process.stdout.write(JSON.stringify({passed:true,checks:['direct Home without login','real PDF import, rotation, reload and download','two browser owner isolation','end starts a fresh private session']}));
 }finally{await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
