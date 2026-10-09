const {chromium}=require('playwright');
const B=process.env.BASE_URL;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
(async()=>{
  const br=await chromium.launch();
  const ctx=()=>br.newContext({permissions:['clipboard-read','clipboard-write']});
  const a=await (await ctx()).newPage(); const errs=[]; a.on('pageerror',e=>errs.push(e.message));
  await a.goto(B+'/chat/'); await a.fill('input[placeholder="Your nickname..."]','Alice'); await a.click('text=Start New Session');
  await a.waitForSelector('text=Live Session');
  const room=new URL(a.url()).searchParams.get('room'); ok(/^[0-9a-f-]{36}$/.test(room||''),'address bar has ?room=<id>');
  ok(await a.locator('input[readonly]').inputValue()===`${B}/chat/?room=${room}`,'empty room shows invite link');
  await a.click('text=🔗 Copy invite link'); await a.waitForSelector('text=✓ Link copied');
  const link=await a.evaluate(()=>navigator.clipboard.readText()); ok(link===`${B}/chat/?room=${room}`,'clipboard has invite link');

  const b=await (await ctx()).newPage(); b.on('pageerror',e=>errs.push(e.message));
  await b.goto(link);
  ok(await b.isVisible("text=You've been invited"),'invitee sees invite banner');
  ok(!(await b.isVisible('text=Start New Session')),'create button hidden for invitee');
  ok(await b.locator('input[placeholder="Session ID or invite link"]').inputValue()===room,'room prefilled');
  await b.fill('input[placeholder="Your nickname..."]','Bob'); await b.press('input[placeholder="Your nickname..."]','Enter');
  await b.waitForSelector('text=Live Session');
  await a.fill('input[placeholder="Transmit a message..."]','hello bob'); await a.press('input[placeholder="Transmit a message..."]','Enter');
  await b.waitForSelector('text=hello bob'); ok(await b.isVisible('text=Alice'),'invitee receives message from Alice');
  await a.waitForSelector('text=hello bob'); ok((await a.locator('.animate-msg-in',{hasText:'hello bob'}).innerText()).startsWith('You'),'sender sees own message as You');

  const c=await (await ctx()).newPage();
  await c.goto(B+'/chat/'); await c.fill('input[placeholder="Your nickname..."]','Carol');
  await c.fill('input[placeholder="Session ID or invite link"]',link); await c.press('input[placeholder="Session ID or invite link"]','Enter');
  await c.waitForSelector('text=hello bob',{state:'detached',timeout:500}).catch(()=>{});
  await c.waitForSelector('text=Live Session'); ok(new URL(c.url()).searchParams.get('room')===room,'pasting full invite link joins room');

  await c.click('text=Exit'); ok(c.url()===B+'/chat/','exit clears ?room from URL');
  await a.reload(); await a.waitForSelector('text=Live Session',{timeout:5000}); ok(!(await a.isVisible("text=You've been invited")),'refresh rejoins the room directly');

  // Secret flow through the UI
  const s=await (await ctx()).newPage(); s.on('pageerror',e=>errs.push(e.message));
  await s.goto(B+'/chat/secret'); await s.fill('textarea','top secret 机密'); await s.fill('input[type=password]','pw1');
  await s.click('text=1 week'); await s.click('text=Create Secret Link');
  const sl=await (await s.waitForSelector('input[readonly]',{timeout:15000})).inputValue();
  const r=await (await ctx()).newPage(); r.on('pageerror',e=>errs.push(e.message)); await r.goto(sl);
  await r.fill('input[type=password]','nope'); await r.click('text=Reveal Secret');
  await r.waitForSelector('text=4 attempts left',{timeout:15000}); ok(true,'secret UI: wrong password shows attempts');
  await r.fill('input[type=password]','pw1'); await r.click('text=Reveal Secret');
  await r.waitForSelector('text=top secret 机密',{timeout:15000}); ok(!r.url().includes('#'),'secret UI: revealed, key removed from URL');
  const r2=await (await ctx()).newPage(); await r2.goto(sl); await r2.waitForSelector('text=already read'); ok(true,'secret UI: second open says already read');
  ok(errs.length===0,'no page errors '+errs.join('; '));
  await br.close();
})().catch(e=>{console.log('FAIL exception',e.message);process.exitCode=1});
