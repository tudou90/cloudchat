const {webkit,chromium,devices}=require('playwright'); const B=process.env.BASE_URL, O=process.env.ARTIFACTS_DIR;
const PNG=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==','base64');
let fails=0; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) fails++};
(async()=>{
 for (const [engine,devName,tag] of [[webkit,'iPhone 13','iphone13'],[webkit,'iPhone SE','iphoneSE'],[chromium,'Pixel 5','pixel5']]) {
  const br=await engine.launch(); const dev=devices[devName]; const ctx=await br.newContext(dev), p=await ctx.newPage(); const errs=[]; p.on('pageerror',e=>errs.push(e.message));
  const ovf=async()=>p.evaluate(()=>document.documentElement.scrollWidth-window.innerWidth);
  for(const [u,n] of [['/','home'],['/news','news'],['/chat/','landing'],['/chat/secret','secret']]){await p.goto(B+u);await p.waitForTimeout(700);ok(await ovf()<=0,`${tag} ${n}: no horizontal overflow`);if(tag==='iphone13')await p.screenshot({path:`${O}/${tag}-${n}.png`,fullPage:true});}
  await p.goto(B+'/chat/'); await p.fill('input[placeholder="Your nickname..."]','Alice'); await p.click('text=Start New Session'); await p.waitForSelector('text=online');
  const q=await (await br.newContext(dev)).newPage(); await q.goto(p.url()); await q.fill('input[placeholder="Your nickname..."]','Bob'); await q.press('input[placeholder="Your nickname..."]','Enter'); await q.waitForSelector('text=online');
  await q.fill('input[placeholder="Transmit a message..."]','Hey Alice! A fairly long message to check wrapping https://example.com/a/very/long/url/without/any/spaces/at/all'); await q.press('input[placeholder="Transmit a message..."]','Enter');
  await q.setInputFiles('input[type=file]',[{name:'quarterly-report-final-v2.pdf',mimeType:'application/pdf',buffer:Buffer.from('%PDF-1.4 x')}]);
  await p.waitForSelector('text=quarterly-report-final-v2.pdf'); await p.fill('input[placeholder="Transmit a message..."]','hi'); await p.press('input[placeholder="Transmit a message..."]','Enter'); await p.waitForTimeout(700);
  ok(await ovf()<=0,`${tag} chat: no horizontal overflow`);
  const panel=await p.locator('.glass.flex-col').boundingBox(); const vh=await p.evaluate(()=>window.innerHeight);
  ok(Math.abs(panel.height-vh)<=1&&panel.y>=0,`${tag} chat fills viewport (${Math.round(panel.height)} vs ${vh})`);
  const hdr=await p.locator('.border-b.border-white\\/10').first().boundingBox(); ok(hdr.height<70,`${tag} header single row (h=${Math.round(hdr.height)})`);
  const exit=await p.locator('text=Exit').boundingBox(); ok(exit.x+exit.width<=dev.viewport.width,`${tag} Exit button on screen`);
  ok(await p.evaluate(()=>getComputedStyle(document.querySelector('input[placeholder="Transmit a message..."]')).fontSize)==='16px',`${tag} chat input 16px (no iOS zoom)`);
  if(tag==='iphone13'){await p.screenshot({path:`${O}/${tag}-chat.png`});await p.click('text=online');await p.waitForTimeout(300);await p.screenshot({path:`${O}/${tag}-members.png`});}
  ok(errs.length===0,`${tag} no page errors`); await br.close();
 }
 // desktop unchanged
 const br=await chromium.launch(); const p=await br.newPage({viewport:{width:1280,height:800}});
 await p.goto(B+'/chat/'); await p.fill('input[placeholder="Your nickname..."]','D'); await p.click('text=Start New Session'); await p.waitForSelector('text=Live Session');
 ok(await p.isVisible('text=🔗 Copy invite link'),'desktop: full labels kept'); const bb=await p.locator('.glass.flex-col').boundingBox(); ok(Math.round(bb.height)===704,'desktop: card height 88vh ('+Math.round(bb.height)+')');
 await br.close(); console.log(fails?`${fails} FAILED`:'ALL OK');
})();
