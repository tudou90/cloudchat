const B=process.env.BASE_URL, W=process.env.BASE_URL.replace(/^http/,'ws');
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const conn=(room,name)=>new Promise((res,rej)=>{const ws=new WebSocket(`${W}/ws/${room}?name=${encodeURIComponent(name)}`);ws.msgs=[];
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>{ws.token=ws.msgs[0].token;ws.id=ws.msgs[0].senderId;res(ws)},200);ws.onerror=rej;});
const newRoom=async()=>(await (await fetch(`${B}/api/rooms`,{method:'POST'})).json()).id;
const up=(room,token,bytes,name,type='application/octet-stream')=>{const f=new FormData();f.append('file',new Blob([bytes],{type}),name);
  return fetch(`${B}/api/rooms/${room}/files`,{method:'POST',body:f,headers:token?{'X-Client-Token':token}:{}})};
const PNG=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==','base64');

const room=await newRoom(), other=await newRoom();
const a=await conn(room,'Alice'), b=await conn(room,'Bob');
ok(a.token&&a.token!==a.id,'welcome has private token distinct from senderId');
ok(!b.msgs.some(m=>m.token&&m.senderId!==b.id),'token never sent to other clients');
ok((await up(room,'',PNG,'a.png')).status===403,'no token -> 403');
ok((await up(room,b.id,PNG,'a.png')).status===403,"someone else's public senderId -> 403");
ok((await up(other,a.token,PNG,'a.png')).status===403,'token for another room -> 403');

let r=await up(room,a.token,PNG,'pic.png','image/png'); const info=await r.json();
ok(r.status===200&&info.mime==='image/png'&&info.size===PNG.length,'upload png -> 200');
await wait(300); const fm=b.msgs.find(m=>m.type==='file');
ok(fm&&fm.sender==='Alice'&&fm.senderId===a.id&&fm.file.url===info.url,'room receives file message from Alice');
r=await fetch(B+info.url); const got=Buffer.from(await r.arrayBuffer());
ok(r.status===200&&got.equals(PNG),'download returns identical bytes');
ok(r.headers.get('content-type')==='image/png'&&r.headers.get('content-disposition').startsWith('inline'),'png served inline as image/png');
ok(r.headers.get('x-content-type-options')==='nosniff'&&r.headers.get('content-security-policy').includes('sandbox'),'nosniff + CSP sandbox');

r=await up(room,a.token,'<script>alert(1)</script>','evil.html','text/html'); const ev=await r.json();
r=await fetch(B+ev.url); ok(r.headers.get('content-type')==='application/octet-stream'&&r.headers.get('content-disposition').startsWith('attachment'),'html forced to download as octet-stream');
r=await up(room,a.token,'<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>','x.svg','image/svg+xml'); const sv=await r.json();
r=await fetch(B+sv.url); ok(r.headers.get('content-disposition').startsWith('attachment'),'svg (client says image/svg+xml) forced to download');
r=await up(room,a.token,'hi','报告 2026.txt'); const cn=await r.json();
r=await fetch(B+cn.url); ok(cn.name==='报告 2026.txt'&&r.headers.get('content-disposition').includes("filename*=utf-8''"),'Chinese filename preserved (RFC 5987)');
r=await up(room,a.token,'x','../../etc/passwd'); ok((await r.json()).name==='passwd','path traversal in name stripped');
ok((await up(room,a.token,new Uint8Array(0),'e.txt')).status===400,'empty file -> 400');
ok((await up(room,a.token,new Uint8Array(10*1024*1024),'max.bin')).status===200,'exactly 10MB -> 200');
ok((await up(room,a.token,new Uint8Array(10*1024*1024+1),'big.bin')).status===413,'10MB + 1 byte -> 413');
ok((await fetch(`${B}/api/rooms/${other}/files/${info.id}`)).status===404,'file id with wrong room -> 404');
ok((await fetch(`${B}/api/rooms/${room}/files/nope`)).status===404,'malformed file id -> 404');

a.send(JSON.stringify({type:'file',content:'x',file:{id:'f',name:'fake',url:'https://evil'}}));await wait(300);
const forged=b.msgs.at(-1); ok(forged.type==='chat'&&!forged.file,'client cannot forge file message via WS');

// quota: 50MB per room
const q=await newRoom(), qa=await conn(q,'Q'); const st=[];
for(let i=0;i<6;i++) st.push((await up(q,qa.token,new Uint8Array(10*1024*1024),`q${i}.bin`)).status);
ok(st.slice(0,5).every(s=>s===200)&&st[5]===413,'room quota: 5x10MB ok, 6th -> 413 ('+st.join()+')');

a.close();await wait(300); ok((await up(room,a.token,PNG,'late.png')).status===403,'token revoked after disconnect');
b.close();qa.close();
