const {chromium}=require('playwright');
const B=process.env.BASE_URL;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const PNG=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==','base64');
(async()=>{
  const br=await chromium.launch(); const errs=[];
  const page=async()=>{const p=await (await br.newContext({acceptDownloads:true})).newPage();p.on('pageerror',e=>errs.push(e.message));return p};
  const a=await page(); await a.goto(B+'/chat/'); await a.fill('input[placeholder="Your nickname..."]','Alice'); await a.click('text=Start New Session'); await a.waitForSelector('text=Live Session');
  const b=await page(); await b.goto(a.url()); await b.fill('input[placeholder="Your nickname..."]','Bob'); await b.press('input[placeholder="Your nickname..."]','Enter'); await b.waitForSelector('text=Live Session');
  await a.waitForTimeout(300);

  await a.setInputFiles('input[type=file]',[{name:'dot.png',mimeType:'image/png',buffer:PNG},{name:'notes 笔记.txt',mimeType:'text/plain',buffer:Buffer.from('hello file')}]);
  const img=await b.waitForSelector('img[alt="dot.png"]'); await b.waitForFunction(i=>i.complete&&i.naturalWidth>0,img);
  ok(true,'picker upload (2 files): image renders for Bob');
  await b.waitForSelector('text=notes 笔记.txt'); ok(await b.isVisible('text=10 B · Download'),'text file shows as card with size');
  const [dl]=await Promise.all([b.waitForEvent('download'),b.click('text=notes 笔记.txt')]);
  const fs=require('fs'); ok(dl.suggestedFilename()==='notes 笔记.txt'&&fs.readFileSync(await dl.path(),'utf8')==='hello file','card click downloads original file & name');
  ok(await a.locator('img[alt="dot.png"]').count()===1,'uploader also sees own image');

  // drag & drop
  await b.evaluate(()=>{const dt=new DataTransfer();dt.items.add(new File(['dropped!'],'drop.txt',{type:'text/plain'}));
    const t=document.querySelector('.glass.rounded-3xl');for(const ev of ['dragenter','drop'])t.dispatchEvent(new DragEvent(ev,{bubbles:true,cancelable:true,dataTransfer:dt}));});
  await a.waitForSelector('text=drop.txt'); ok(await a.isVisible('text=Bob'),'drag & drop upload reaches Alice');

  // oversize rejected client-side
  await a.setInputFiles('input[type=file]',[{name:'huge.bin',mimeType:'application/octet-stream',buffer:Buffer.alloc(10*1024*1024+1)}]);
  await a.waitForSelector('text=huge.bin is larger than 10 MB'); ok(true,'oversize file rejected with message');
  ok(errs.length===0,'no page errors '+errs.join('; '));
  await br.close();
})().catch(e=>{console.log('FAIL exception',e.message);process.exitCode=1;process.exit(1)});
