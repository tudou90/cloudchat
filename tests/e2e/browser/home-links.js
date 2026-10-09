const {chromium,webkit,devices}=require('playwright'); const B=process.env.BASE_URL, O=process.env.ARTIFACTS_DIR;
let f=0; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) f++};
(async()=>{
 const br=await chromium.launch(); const p=await br.newPage(); const errs=[]; p.on('pageerror',e=>errs.push(e.message));
 const home=async(start,sel,name)=>{await p.goto(B+start); await p.click(sel); await p.waitForURL(B+'/'); ok(await p.isVisible('h1:has-text("Free temporary chat rooms")'),`${name} -> home page`)};
 await home('/chat/','a[title="CloudChat home"]','chat landing logo');
 await home('/chat/','text=← Home','chat landing "← Home"');
 await home('/chat/?room=00000000-0000-0000-0000-000000000000','text=← Home','invite screen "← Home"');
 await home('/chat/secret','text=← Home','secret create "← Home"');
 await home('/chat/secret/AAAAAAAAAAAAAAAAAAAAAA#x','text=← Home','secret reveal "← Home"');
 await p.goto(B+'/chat/secret'); await p.click('text=Go to chat'); await p.waitForURL(B+'/chat/'); ok(true,'secret create "Go to chat" still works');
 await p.goto(B+'/chat/'); await p.fill('input[placeholder="Your nickname..."]','A'); await p.click('text=Start New Session'); await p.waitForSelector('text=Live Session');
 ok(await p.locator('a[href="/"]:visible').count()===0,'no home link inside the chat room');
 await p.click('text=Exit'); ok(await p.isVisible('text=← Home'),'after Exit, home link is back');
 ok(errs.length===0,'no page errors'); await br.close();
 const wb=await webkit.launch(); const m=await (await wb.newContext(devices['iPhone SE'])).newPage();
 for(const u of ['/chat/','/chat/secret']){await m.goto(B+u); await m.waitForTimeout(500); ok(await m.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`320px ${u}: no overflow`);}
 await m.goto(B+'/chat/'); await m.waitForTimeout(600); await m.screenshot({path:O+'/se-landing.png',fullPage:true}); await wb.close();
 console.log(f?f+' FAILED':'ALL OK');
})();
