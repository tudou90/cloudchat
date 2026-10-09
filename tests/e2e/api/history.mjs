const limit=+process.env.HISTORY_LIMIT; const B=process.env.BASE_URL, W=B.replace(/^http/,'ws');
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+`[limit=${limit}] `+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const conn=(room,name)=>new Promise((res,rej)=>{const ws=new WebSocket(`${W}/ws/${room}?name=${name}`);ws.msgs=[];
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>res(ws),300);ws.onerror=rej;});
const room=(await (await fetch(`${B}/api/rooms`,{method:'POST'})).json()).id;
const a=await conn(room,'Alice');
for(let i=1;i<=7;i++){a.send(JSON.stringify({content:'m'+i}));await wait(30);}
const f=new FormData();f.append('file',new Blob(['doc']),'d.txt');
await fetch(`${B}/api/rooms/${room}/files`,{method:'POST',body:f,headers:{'X-Client-Token':a.msgs[0].token}}); await wait(300);
ok(a.msgs.filter(m=>m.type==='chat').every(m=>m.id),'live messages carry ids');
const b=await conn(room,'Bob'); const h=b.msgs.find(m=>m.type==='history');
if(limit===0){ ok(!h,'no history sent when disabled'); }
else {
  ok(h&&h.messages.length===Math.min(limit,8),`history has ${h?.messages.length} msgs`);
  ok(h.messages.at(-1).type==='file'&&h.messages.at(-1).file.name==='d.txt','file message included, newest last');
  ok(h.messages.at(-2).content==='m7','ordered oldest->newest');
  ok(!JSON.stringify(h).includes(a.msgs[0].token),'history never leaks upload tokens');
}
a.close();b.close();
