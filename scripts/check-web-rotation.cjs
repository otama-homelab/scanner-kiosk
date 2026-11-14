const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.setDefaultTimeout(10000);
 const id='document_rotation_001',pageID='page_rotation_00001',errors=[],checks=[];
 const doc={id,revision:1,purpose:'document',state:'review',created_at:'2026-11-14T03:00:00Z',expires_at:new Date(Date.now()+3600000).toISOString(),active_job_id:null,output:null,pages:[]};
 let raster;const reply=(r,json)=>r.fulfill({status:200,headers:{Date:new Date().toUTCString()},json});
 try{
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/api/session',r=>reply(r,{authenticated:true,authenticate_type:'guest',principal_kind:'guest'}));
  await page.route('**/api/documents/'+id,r=>reply(r,doc));
  await page.route('**/api/documents/'+id+'/pages/'+pageID,r=>{assert.equal(r.request().method(),'PATCH');assert.equal(r.request().headers()['if-match'],'"'+doc.revision+'"');doc.pages[0].rotation=r.request().postDataJSON().rotation;doc.revision++;return reply(r,{revision:doc.revision})});
  await page.route('**/api/documents/'+id+'/pages/'+pageID+'/thumbnail',r=>r.fulfill({status:200,contentType:'image/png',body:raster}));
  await page.route('**/api/documents/'+id+'/pages/'+pageID+'/preview',r=>r.fulfill({status:200,contentType:'image/png',body:raster}));
  await page.goto(process.env.SCANNER_TEST_ORIGIN+'/#/document/'+id);await page.locator('h1[data-document-title]').waitFor();
  for(const source of ['scan','photo'])for(const [width,height] of [[800,1200],[1200,800],[2000,300],[300,2000]]){
   raster=Buffer.from(await page.evaluate(([w,h])=>{const c=document.createElement('canvas');c.width=w;c.height=h;const x=c.getContext('2d');x.fillStyle='#fff';x.fillRect(0,0,w,h);x.strokeStyle='#263449';x.lineWidth=8;x.strokeRect(4,4,w-8,h-8);['#df4848','#2e9468','#4268dc','#c48d22'].forEach((color,i)=>{x.fillStyle=color;x.fillRect(i%2*(w-60),Math.floor(i/2)*(h-60),60,60)});return c.toDataURL('image/png').split(',')[1]},[width,height]),'base64');
   doc.pages=[{id:pageID,ordinal:0,rotation:0,source_type:source,...(source==='photo'?{photo:{width,height,sha256:'a'.repeat(64)}}:{})}];
   await page.reload();await page.locator('h1[data-document-title]').waitFor();
   for(const viewport of [{width:320,height:844},{width:390,height:844},{width:768,height:1024},{width:1440,height:900},{width:1280,height:650}]){
    await page.setViewportSize(viewport);
    for(const rotation of [90,180,270,0]){
     await page.locator('.page-actions [data-page-action="rotate"]').click();
     await page.waitForFunction(rotation=>{const img=document.querySelector('.preview-card:not([hidden]) img');return img?.complete&&img.naturalWidth>0&&img.style.transform.includes('rotate('+rotation+'deg)')},rotation);
     const geometry=await page.locator('.preview-card:not([hidden])').evaluate(card=>{const rect=e=>{const r=e.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,right:r.right,bottom:r.bottom}};return {card:rect(card),image:rect(card.querySelector('img')),actions:rect(card.querySelector('.page-actions')),overflow:document.documentElement.scrollWidth>innerWidth}});
     const {card,image,actions}=geometry,label=`${source} ${width}x${height} ${viewport.width}x${viewport.height} ${rotation}`;
     try{
      assert(!geometry.overflow,label+' horizontal overflow');
      assert(image.width>0&&image.height>0,label+' image is visible');
      assert(image.x>=card.x-1&&image.right<=card.right+1&&image.y>=card.y-1&&image.bottom<=card.bottom+1,label+' complete image fits card');
      assert(viewport.width>960?image.right<=actions.x+1:image.bottom<=actions.y+1,label+' image does not overlap actions');
      const ratio=rotation%180?height/width:width/height;assert(Math.abs(image.width/image.height-ratio)<.01,label+' rotated aspect ratio is preserved');
     }catch(error){if(process.env.SCANNER_TEST_SCREENSHOT)await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT,fullPage:true});console.error(JSON.stringify({label,geometry}));throw error}
     assert.equal(await page.evaluate(()=>document.activeElement?.dataset.pageAction),'rotate',label+' rotation keeps keyboard focus');
     if(process.env.SCANNER_TEST_SCREENSHOT&&source==='scan'&&width===800&&rotation===90&&[390,1440].includes(viewport.width))await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT.replace('.png','-rotation-'+viewport.width+'.png'),fullPage:true});
     checks.push(label);
    }
    if(source==='scan'&&width===800&&[390,1440].includes(viewport.width)){
     const viewer=page.locator('.preview-card:not([hidden]) .document-viewer'),port=viewer.locator('.document-viewer-viewport');
     const center=()=>port.evaluate(e=>{const v=e.getBoundingClientRect(),s=e.querySelector('.document-viewer-surface').getBoundingClientRect();return {x:(v.x+e.clientWidth/2-s.x)/s.width,y:(v.y+e.clientHeight/2-s.y)/s.height}});
     for(let i=0;i<8;i++)await viewer.getByRole('button',{name:'Zoom in',exact:true}).click();
     await page.waitForFunction(()=>document.querySelector('.preview-card:not([hidden]) img')?.getAttribute('src')?.endsWith('/preview'));
     assert.equal(await viewer.locator('output').textContent(),'300%');
     await port.evaluate(e=>e.scrollTo((e.scrollWidth-e.clientWidth)*.55,(e.scrollHeight-e.clientHeight)*.45));
     await page.waitForFunction(()=>document.querySelector('.preview-card:not([hidden]) .document-viewer-viewport').scrollTop>0);
     const before=await center();await viewer.getByRole('button',{name:'Zoom in',exact:true}).click();const after=await center();
     assert(Math.abs(before.x-after.x)<.01&&Math.abs(before.y-after.y)<.01,'zoom preserves the point being read, including fitted margins');
     const b=await port.boundingBox(),position=await port.evaluate(e=>({left:e.scrollLeft,top:e.scrollTop}));await page.mouse.move(b.x+b.width/2,b.y+b.height/2);await page.mouse.down();await page.mouse.move(b.x+b.width/2-40,b.y+b.height/2-40,{steps:4});await page.mouse.up();
     const moved=await port.evaluate(e=>({left:e.scrollLeft,top:e.scrollTop}));assert(moved.left>position.left+30&&moved.top>position.top+30,'mouse drag moves the enlarged original');
     await port.evaluate(e=>e.scrollTo(e.scrollWidth,e.scrollHeight));assert(await port.evaluate(e=>Math.abs(e.scrollLeft+e.clientWidth-e.scrollWidth)<=1&&Math.abs(e.scrollTop+e.clientHeight-e.scrollHeight)<=1),'far image edges remain reachable');
     await viewer.getByRole('button',{name:'Fit page',exact:true}).click();assert.equal(await viewer.locator('output').textContent(),'100%');assert(await viewer.getByRole('button',{name:'Zoom out',exact:true}).isDisabled());assert(await port.evaluate(e=>e.scrollLeft===0&&e.scrollTop===0));
     await port.focus();await page.keyboard.press('+');assert.equal(await viewer.locator('output').textContent(),'125%');await page.keyboard.press('0');assert.equal(await viewer.locator('output').textContent(),'100%');
     await page.keyboard.press('Tab');const zoomButton=viewer.getByRole('button',{name:'Zoom in',exact:true});assert(await zoomButton.evaluate(e=>document.activeElement===e),'keyboard reaches zoom controls');const focus=await zoomButton.evaluate(e=>({style:getComputedStyle(e).outlineStyle,width:parseFloat(getComputedStyle(e).outlineWidth)}));assert(focus.style==='solid'&&focus.width>=3,'zoom control has a visible keyboard outline');
     assert(await port.evaluate(e=>e.dispatchEvent(new KeyboardEvent('keydown',{key:'+',ctrlKey:true,bubbles:true,cancelable:true}))),'viewer does not suppress browser Ctrl zoom');
     assert(['manipulation','pan-x pan-y pinch-zoom'].includes(await port.evaluate(e=>getComputedStyle(e).touchAction)),'browser touch zoom is retained');checks.push('center-preserving zoom, high-resolution source, drag, edges, keyboard and fit '+viewport.width);
    }
   }
  }
  await page.route('**/api/documents/'+id+'/pages/'+pageID+'/thumbnail',r=>r.fulfill({status:404}));await page.route('**/api/documents/'+id+'/pages/'+pageID+'/preview',r=>r.fulfill({status:404}));await page.reload();
  await page.getByText('The image preview is unavailable. You can still save the document.',{exact:true}).waitFor();
  const unavailable=page.locator('.preview-card:not([hidden]) .document-viewer');assert(await unavailable.getByRole('button',{name:'Zoom in',exact:true}).isDisabled());await unavailable.locator('.document-viewer-viewport').focus();await page.keyboard.press('+');assert.equal(await unavailable.locator('output').textContent(),'100%');assert(await unavailable.getByRole('button',{name:'Zoom in',exact:true}).isDisabled());assert(await page.getByRole('button',{name:'Save PDF',exact:true}).isEnabled());checks.push('failed source keeps zoom disabled even from keyboard and preserves Save');
  assert.deepEqual(errors,[]);console.log(JSON.stringify({passed:true,checks,scope:'Synthetic raster routes; real layout and rotate UI. Physical iPhone and image export are not verified here.'}));
 }finally{await context.close();await browser.close()}
})().catch(error=>{console.error(error.stack||error.message);process.exit(1)});
