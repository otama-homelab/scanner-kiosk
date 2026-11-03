const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 try {
  const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();const errors=[];
  page.on('pageerror',error=>errors.push(error.message));
  await page.addInitScript(()=>document.addEventListener('securitypolicyviolation',event=>window.__csp=(window.__csp||[]).concat(event.violatedDirective)));
  const origin=process.env.SCANNER_TEST_ORIGIN;await page.goto(origin);
  await page.getByRole('button',{name:'Start session',exact:true}).click();await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  assert.equal((await page.request.post(origin+'/__fixture/native-seed')).status(),200);
  const metadata=await (await page.request.get(origin+'/api/capabilities')).json();assert.equal(metadata.defaults.ocr.provider_id,'tesseract');assert.deepEqual(metadata.defaults.ocr.languages,['ja','en']);
  const plugin=metadata.ocr_plugins.find(plugin=>plugin.id==='tesseract');assert.ok(plugin);const model=plugin.models.find(model=>model.id===metadata.defaults.ocr.model_id);assert.ok(model);
  await page.goto(origin+'/#/document/document_native_001');await page.getByRole('heading',{name:'Document',exact:true}).waitFor();
  await page.getByRole('button',{name:'Output settings',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#ocr-model')?.value==='0');
  assert.equal(await page.locator('#ocr-enabled').isChecked(),false);await page.locator('#ocr-enabled').check();assert.equal(await page.locator('.ocr-languages input:checked').count(),2);
  assert.equal(await page.locator('#ocr-quality').inputValue(),metadata.defaults.ocr.quality_preset);await page.selectOption('#output-choice','lossless');
  const admission=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/documents/document_native_001/render'&&response.request().method()==='POST');
  await page.getByRole('dialog').getByRole('button',{name:'Create PDF',exact:true}).click();const response=await admission;assert.equal(response.status(),202);const job=await response.json();
  assert.deepEqual(response.request().postDataJSON(),{format:'pdf',pdf_encoding:'lossless',ocr:{enabled:true,provider_id:'tesseract',model_id:model.id,languages:['ja','en'],quality_preset:metadata.defaults.ocr.quality_preset}});
  await page.getByText('Completed',{exact:true}).waitFor({timeout:60000});await page.getByRole('button',{name:'Open',exact:true}).click();
  const downloaded=page.waitForEvent('download');await page.getByRole('link',{name:'Download',exact:true}).click();const download=await downloaded;
  assert.equal(download.suggestedFilename(),'scan.pdf');await download.saveAs(process.env.SCANNER_TEST_DOWNLOAD);
  assert.deepEqual(errors,[]);assert.deepEqual(await page.evaluate(()=>window.__csp||[]),[]);
  console.log(JSON.stringify({passed:true,job_id:job.id,model_version:model.version,checks:['real worker catalog and configured Japanese/English preference','browser OCR command accepted with server-pinned model','daemon recognition to succeeded job','Chromium native PDF download','no JavaScript/CSP errors']}));
  await context.close();
 }finally{await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
