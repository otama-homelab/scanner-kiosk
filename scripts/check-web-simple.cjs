const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE,chromiumSandbox:true});
 const checks=[],errors=[],password='Synthetic browser login 2026!';
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(process.env.SCANNER_TEST_ORIGIN);await page.getByRole('heading',{name:'Sign in',exact:true}).waitFor();
  assert.equal(await page.locator('html').getAttribute('lang'),'en');assert.equal(await page.locator('#login-password').getAttribute('type'),'password');assert.equal(await page.locator('#login-password').getAttribute('autocomplete'),'current-password');
  for(const width of [320,390,768,1440]){
   await page.setViewportSize({width,height:900});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  }
  checks.push('English default, labeled password-manager fields and responsive login');
  await page.getByLabel('Username',{exact:true}).fill('Alice');await page.getByLabel('Password',{exact:true}).fill('wrong password');await page.getByRole('button',{name:'Sign in',exact:true}).click();
  await page.getByText('Incorrect username or password.',{exact:true}).waitFor();assert.equal(await page.locator('#login-password').inputValue(),'');assert.equal(await page.locator('#login-username').inputValue(),'Alice');
  checks.push('Rejected credentials clear password and retain username');
  await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByRole('heading',{name:'Home',exact:true}).waitFor();
  const session=await(await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/session')).json();assert.equal(session.authenticate_type,'simple');assert.equal(session.principal_kind,'simple');assert.equal(session.can_administer,false);assert.equal(session.auth_mode,undefined);
  await page.getByRole('button',{name:'History',exact:true}).click();await page.getByRole('button',{name:/^Open / }).waitFor();
  checks.push('Real simple login/session and owner history through the embedded API');
  await page.reload();await page.getByRole('button',{name:/^Open / }).waitFor();
  const storage=await page.evaluate(()=>JSON.stringify({local:{...localStorage},session:{...sessionStorage}}));assert(!storage.includes(password));
  checks.push('Reload preserves server session without password storage');
  const otherContext=await browser.newContext({ignoreHTTPSErrors:true});const other=await otherContext.newPage();
  await other.goto(process.env.SCANNER_TEST_ORIGIN);await other.getByLabel('Username',{exact:true}).fill('Bob');await other.getByLabel('Password',{exact:true}).fill(password);await other.getByRole('button',{name:'Sign in',exact:true}).click();await other.getByRole('heading',{name:'Home',exact:true}).waitFor();
  assert.equal((await other.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/documents/document_simple_001')).status(),404);await other.getByRole('button',{name:'History',exact:true}).click();await other.getByText('No documents',{exact:true}).waitFor();
  checks.push('Another password-authenticated user cannot open the first history');
  await page.getByRole('button',{name:'Sign out',exact:true}).click();await page.getByRole('heading',{name:'Sign in',exact:true}).waitFor();
  assert.equal((await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/documents')).status(),401);assert.equal((await other.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/session')).status(),200);
  checks.push('Sign-out removes this browser authority without disabling another user');
  await context.clearCookies();await page.getByLabel('Username',{exact:true}).fill('alice');await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Reset this session to sign in again.',{exact:true}).waitFor();
  await page.getByRole('button',{name:'Reset session',exact:true}).click();await page.getByRole('button',{name:'Reset session',exact:true}).waitFor({state:'hidden'});await page.getByRole('heading',{name:'Sign in',exact:true}).waitFor();assert.equal(await page.locator('#login-password').inputValue(),'');assert.equal(await page.getByRole('button',{name:'Reset session',exact:true}).isVisible(),false);
  checks.push('Expired/missing cookie can reset and bootstrap without replaying credentials');
  await page.selectOption('#locale','ja');await page.getByRole('heading',{name:'ログイン',exact:true}).waitFor();await page.getByLabel('ユーザー名',{exact:true}).fill('alice');await page.getByLabel('パスワード',{exact:true}).fill(password);
  await page.selectOption('#locale','en');await page.getByRole('heading',{name:'Sign in',exact:true}).waitFor();assert.equal(await page.locator('#login-password').inputValue(),'');
  let release,seen;const released=new Promise(resolve=>release=resolve),intercepted=new Promise(resolve=>seen=resolve);let attempts=0;
  await page.route('**/auth/simple/login',async route=>{attempts++;seen();await released;await route.abort('aborted').catch(()=>{})});
  await page.getByLabel('Username',{exact:true}).fill('alice');await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await intercepted;
  assert.equal(await page.locator('#login-password').getAttribute('readonly'),'');await page.selectOption('#locale','ja');await page.getByRole('heading',{name:'ログイン',exact:true}).waitFor();release();await page.unroute('**/auth/simple/login');assert.equal(await page.locator('#login-password').inputValue(),'');assert.equal(attempts,1);
  assert.equal((await(await page.request.get(process.env.SCANNER_TEST_ORIGIN+'/api/session')).json()).authenticated,false);
  checks.push('Pending sign-in aborts on view change and is never automatically replayed');
  await page.selectOption('#locale','ja');await page.getByRole('heading',{name:'ログイン',exact:true}).waitFor();await page.setViewportSize({width:390,height:844});await page.screenshot({path:process.env.SCANNER_TEST_SCREENSHOT,fullPage:true});
  checks.push('Japanese login labels and view-change password disposal');
  assert.deepEqual(errors,[]);await otherContext.close();await context.close();
  process.stdout.write(JSON.stringify({passed:true,checks}));
 }finally{await browser.close()}
})().catch(e=>{console.error(e.stack||e.message);process.exit(1)});
