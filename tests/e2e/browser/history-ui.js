const {chromium}=require('playwright');
const B=process.env.BASE_URL;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const PNG=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==','base64');
const say=async(p,t)=>{await p.fill('input[placeholder="Transmit a message..."]',t);await p.press('input[placeholder="Transmit a message..."]','Enter')};
const bubble=(p,t)=>p.locator('.animate-msg-in',{hasText:t});
(async()=>{
  const br=await chromium.launch(); const errs=[];
  const ctxA=await br.newContext(), ctxB=await br.newContext();
  const a=await ctxA.newPage(); a.on('pageerror',e=>errs.push(e.message));
  await a.goto(B+'/chat/'); await a.fill('input[placeholder="Your nickname..."]','Alice'); await a.click('text=Start New Session'); await a.waitForSelector('text=Live Session'); await a.waitForTimeout(300);
  await say(a,'alice-1'); await a.setInputFiles('input[type=file]',[{name:'dot.png',mimeType:'image/png',buffer:PNG}]); await a.waitForSelector('img[alt="dot.png"]');
  const b=await ctxB.newPage(); b.on('pageerror',e=>errs.push(e.message));
  await b.goto(a.url()); await b.fill('input[placeholder="Your nickname..."]','Bob'); await b.press('input[placeholder="Your nickname..."]','Enter'); await b.waitForSelector('text=Live Session');
  await b.waitForSelector('text=alice-1'); ok(await b.locator('img[alt="dot.png"]').count()===1,'late joiner Bob sees earlier text + image');
  await say(b,'bob-1'); await a.waitForSelector('text=bob-1');

  await a.reload(); await a.waitForSelector('text=Live Session',{timeout:5000});
  ok(!(await a.isVisible("text=You've been invited")),'reload auto-rejoins (no invite screen)');
  await a.waitForSelector('text=bob-1');
  ok(await bubble(a,'alice-1').count()===1&&await bubble(a,'bob-1').count()===1,'history restored, no duplicates');
  ok((await bubble(a,'alice-1').innerText()).includes('You'),"own old message still shown as 'You'");
  ok((await bubble(a,'alice-1').getAttribute('class')).includes('justify-end'),'own old message right-aligned');
  ok((await bubble(a,'bob-1').innerText()).includes('Bob'),"Bob's message still attributed to Bob");
  ok(await a.locator('img[alt="dot.png"]').count()===1,'image restored after reload');
  await say(a,'alice-2'); await b.waitForSelector('text=alice-2');
  ok((await bubble(b,'alice-2').innerText()).includes('Alice')&&(await bubble(b,'alice-2').getAttribute('class')).includes('justify-start'),'Bob sees post-reload message as Alice');

  ok(!(await a.evaluate(()=>document.cookie)).includes('cc_identity'),'identity cookie hidden from JS (httpOnly)');
  const ck=(await ctxA.cookies()).find(c=>c.name==='cc_identity'); ok(ck&&ck.httpOnly&&ck.sameSite==='Lax','cookie is httpOnly + SameSite=Lax');

  const a2=await ctxA.newPage(); await a2.goto(a.url()); await a2.waitForSelector("text=You've been invited"); ok(true,'new tab with link shows join screen (no auto-join)');
  const link=a.url(); await a.click('text=Exit'); await a.goto(link);
  await a.waitForSelector("text=You've been invited",{timeout:5000}); ok(true,'after Exit, link shows join screen (no auto-join)');
  ok(errs.length===0,'no page errors '+errs.join('; ')); await br.close();
})().catch(e=>{console.log('FAIL exception',e.message);process.exit(1)});
