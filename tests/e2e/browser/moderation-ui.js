// A moderator closes a room while people are in it.
const {chromium}=require('playwright'); const {execFileSync}=require('child_process');
const B=process.env.BASE_URL; let f=0; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) f++};
(async()=>{const br=await chromium.launch(); const errs=[];
 const a=await (await br.newContext()).newPage(), b=await (await br.newContext()).newPage(); [a,b].forEach(p=>p.on('pageerror',e=>errs.push(e.message)));
 await a.goto(B+'/chat/'); await a.fill('input[placeholder="Your nickname..."]','Alice'); await a.click('text=Start New Session'); await a.waitForSelector('text=Live Session');
 await b.goto(a.url()); await b.fill('input[placeholder="Your nickname..."]','Bob'); await b.press('input[placeholder="Your nickname..."]','Enter'); await b.waitForSelector('text=Live Session');
 const box='input[placeholder="Transmit a message..."]'; await a.fill(box,'bad content'); await a.press(box,'Enter'); await b.waitForSelector('text=bad content');
 execFileSync(process.env.SERVER_BIN,['admin','room','delete',a.url(),'--yes'],{env:process.env});
 for(const [p,n] of [[a,'Alice'],[b,'Bob']]){
   await p.waitForSelector('text=closed for violating our Terms',{timeout:5000}); ok(true,`${n} sees the "closed" notice`);
   ok(await p.locator('.animate-msg-in',{hasText:'bad content'}).count()===0,`${n}: reported content removed from the screen`);
 }
 await a.waitForTimeout(5000);
 ok(!(await a.isVisible('text=Live Session'))&&!(await a.isVisible('text=Reconnecting')),'no reconnect attempts after closing');
 ok(errs.length===0,'no page errors'); await br.close(); process.exit(f?1:0);})().catch(e=>{console.log('FAIL exception',e.message);process.exit(1)});
