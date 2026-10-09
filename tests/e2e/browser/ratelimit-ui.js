const {chromium}=require('playwright'); const B=process.env.BASE_URL;
let f=0; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) f++};
(async()=>{const br=await chromium.launch(); const p=await br.newPage(); const errs=[]; p.on('pageerror',e=>errs.push(e.message));
 const dialogs=[]; p.on('dialog',d=>{dialogs.push(d.message());d.accept()});
 await p.goto(B+'/chat/'); await p.fill('input[placeholder="Your nickname..."]','Flooder'); await p.click('text=Start New Session'); await p.waitForSelector('text=Live Session'); await p.waitForTimeout(300);
 const box='input[placeholder="Transmit a message..."]';
 for(let i=0;i<16;i++){await p.fill(box,'msg '+i); await p.press(box,'Enter');}
 await p.waitForSelector("text=You're sending messages too fast",{timeout:5000}); ok(true,'flooding shows "too fast" notice above the input');
 const shown=await p.locator('.animate-msg-in').count(); ok(shown>=10&&shown<16,`only ${shown} of 16 delivered`);
 await p.waitForTimeout(5600); ok(!(await p.isVisible("text=You're sending messages too fast")),'notice disappears after a few seconds');
 // room creation limit -> alert with server explanation (this page already created 1 room)
 for(let i=0;i<10;i++){await p.click('text=Exit'); await p.click('text=Start New Session'); await p.waitForTimeout(150);}
 ok(dialogs.some(d=>/Too many requests\. Please try again in/.test(d)),'room-creation limit shows server message: '+JSON.stringify(dialogs.at(-1)));
 ok(errs.length===0,'no page errors'); await br.close(); console.log(f?f+' FAILED':'ALL OK');})();
