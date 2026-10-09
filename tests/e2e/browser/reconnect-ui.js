// Automatic reconnect after a server restart, and the "session ended" state.
// Starts and restarts its own server.
const {chromium}=require('playwright'); const {spawn,execSync}=require('child_process');
const B=process.env.BASE_URL; let f=0; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) f++};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const start=async()=>{const p=spawn(process.env.SERVER_BIN,[],{cwd:process.env.REPO_ROOT,env:{...process.env,SERVER_ADDR:':'+new URL(B).port,RATE_LIMIT:'false'},stdio:'ignore'});
  for(let i=0;i<50;i++){try{if((await fetch(B+'/healthz')).ok)return p}catch{} await wait(100)} throw new Error('server did not start')};
const stop=p=>new Promise(r=>{p.once('exit',r);p.kill('SIGTERM')});
(async()=>{let srv=await start(); const br=await chromium.launch(); const errs=[];
 const a=await (await br.newContext()).newPage(); a.on('pageerror',e=>errs.push(e.message)); a.on('console',m=>{if(m.type()==='error'&&/Content Security Policy|Refused/.test(m.text()))errs.push(m.text())});
 await a.goto(B+'/chat/'); await a.fill('input[placeholder="Your nickname..."]','Alice'); await a.click('text=Start New Session'); await a.waitForSelector('text=Live Session');
 const box='input[placeholder="Transmit a message..."]'; await a.fill(box,'before restart'); await a.press(box,'Enter'); await a.waitForSelector('text=before restart');
 await stop(srv); await a.waitForSelector('text=Reconnecting…',{timeout:5000}); ok(true,'server stops -> header shows "Reconnecting…"');
 await wait(1500); srv=await start(); await a.waitForSelector('text=Live Session',{timeout:20000}); ok(true,'server back -> reconnects on its own');
 ok(await a.locator('.animate-msg-in',{hasText:'before restart'}).count()===1,'earlier message still shown once (no duplicate from history)');
 await a.fill(box,'after restart'); await a.press(box,'Enter'); await a.waitForSelector('text=after restart'); ok(true,'can send after reconnecting');
 ok(!(await a.isVisible('text=Connection lost')),'reconnect notice cleared');
 // Room destroyed while disconnected -> "session has ended", no endless retries
 await stop(srv); await a.waitForSelector('text=Reconnecting…'); execSync(`${process.env.REDIS_CLI} flushdb`); srv=await start();
 await a.waitForSelector('text=This session has ended',{timeout:20000}); ok(true,'room deleted meanwhile -> "This session has ended"');
 ok(errs.length===0,'no page errors or CSP violations '+errs.join('; '));
 await br.close(); await stop(srv); process.exit(f?1:0);})().catch(async e=>{console.log('FAIL exception',e.message);process.exit(1)});
