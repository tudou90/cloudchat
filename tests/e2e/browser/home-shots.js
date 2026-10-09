const {chromium,webkit,devices}=require('playwright'); const B=process.env.BASE_URL, O=process.env.ARTIFACTS_DIR;
let f=0; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) f++};
(async()=>{
 const br=await chromium.launch(); const p=await br.newPage({viewport:{width:1280,height:800}}); const errs=[],bad=[]; p.on('pageerror',e=>errs.push(e.message)); p.on('console',m=>{if(m.type()==='error')errs.push(m.text())});
 p.on('response',r=>{if(r.status()>=400)bad.push(r.status()+' '+r.url())});
 for(const u of ['/','/news']){await p.goto(B+u,{waitUntil:'networkidle'}); await p.waitForTimeout(500); await p.screenshot({path:`${O}/desk${u==='/'?'-home':'-news'}.png`,fullPage:true});}
 await p.goto(B+'/'); await p.click('nav >> text=FAQ'); await p.waitForTimeout(1500); ok(await p.evaluate(()=>Math.abs(document.getElementById('faq').getBoundingClientRect().top)<60),'nav "FAQ" scrolls to FAQ section');
 const d=p.locator('details').first(); await d.locator('summary').click(); ok(await d.evaluate(e=>e.open),'FAQ item expands');
 for(const [sel,url] of [['text=Start a free chat room →','/chat/'],['main >> text=Send a secret note','/chat/secret']]){await p.goto(B+'/'); await p.click(sel+' >> nth=0'); await p.waitForURL(B+url); ok(true,`CTA "${sel.split('=').pop()}" -> ${url}`);}
 ok(errs.length===0,'no console/page errors '+errs.join('; ')); ok(bad.length===0,'no failed requests '+bad.join(', '));
 await br.close();
 for(const [eng,dev] of [[webkit,'iPhone SE'],[webkit,'iPhone 13'],[chromium,'Pixel 5']]){const b=await eng.launch();const m=await (await b.newContext(devices[dev])).newPage();
  for(const u of ['/','/news']){await m.goto(B+u,{waitUntil:'networkidle'});await m.waitForTimeout(400);ok(await m.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`${dev} ${u}: no horizontal overflow`);}
  if(dev==='iPhone 13'){await m.goto(B+'/',{waitUntil:'networkidle'});await m.waitForTimeout(400);await m.screenshot({path:O+'/m-home.png',fullPage:true});}
  await b.close();}
 console.log(f?f+' FAILED':'ALL OK');
})();
