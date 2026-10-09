import { execSync } from 'node:child_process';
const B=process.env.BASE_URL, mode=process.argv[2];
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+`[${mode}] `+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const H=ip=>ip?{'X-Forwarded-For':ip}:{};
const post=(u,ip,body)=>fetch(B+u,{method:'POST',headers:{...H(ip),...(body&&typeof body==='string'?{'content-type':'application/json'}:{})},body});
const newRoom=async ip=>(await (await post('/api/rooms',ip)).json()).id;
const conn=(room,ip,name='u')=>new Promise(res=>{const ws=new WebSocket(`${B.replace('http','ws')}/ws/${room}?name=${name}`,{headers:H(ip)});ws.msgs=[];ws.opened=false;
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>{ws.opened=true;setTimeout(()=>res(ws),150)};ws.onerror=()=>res(ws);ws.onclose=()=>res(ws);});
const up=(room,tok,ip,bytes,name)=>{const f=new FormData();f.append('file',new Blob([bytes]),name);return fetch(`${B}/api/rooms/${room}/files`,{method:'POST',body:f,headers:{'X-Client-Token':tok,...H(ip)}})};

if(mode==='trusted'){
  // room creation: 10/min per client
  const st=[];for(let i=0;i<11;i++)st.push(await post('/api/rooms','198.51.100.1'));
  ok(st.slice(0,10).every(r=>r.status===200)&&st[10].status===429,'11th room in a minute -> 429');
  const j=await st[10].json(); ok(+st[10].headers.get('retry-after')>0&&/try again in/.test(j.message),`429 has Retry-After + message: "${j.message}"`);
  ok((await post('/api/rooms','198.51.100.2')).status===200,'another client is unaffected');
  // IPv6 /64 grouping
  for(let i=0;i<10;i++) await post('/api/rooms',`2001:db8:1:2::${i+1}`);
  ok((await post('/api/rooms','2001:db8:1:2:ffff::99')).status===429,'IPv6: rotating addresses inside one /64 shares the limit');
  ok((await post('/api/rooms','2001:db8:1:3::1')).status===200,'IPv6: a different /64 is separate');
  const ttl=+execSync(`${process.env.REDIS_CLI} pttl rl:room-create:198.51.100.1`).toString(); ok(ttl>0&&ttl<=60000,'limit counters expire with their window');

  // concurrent connections: 20 per client
  const room=await newRoom('198.51.100.3');
  const conns=[];for(let i=0;i<21;i++)conns.push(await conn(room,'198.51.100.4'));
  ok(conns.slice(0,20).every(w=>w.opened)&&!conns[20].opened,'21st concurrent connection refused');
  conns[0].close();await wait(400); const again=await conn(room,'198.51.100.4'); ok(again.opened,'closing one frees a slot');
  conns.concat(again).forEach(w=>w.close()); await wait(400);

  // chat flood: burst 10 then 2/s
  const r2=await newRoom('198.51.100.5'); const a=await conn(r2,'198.51.100.6','spammer'), b=await conn(r2,'198.51.100.7','reader');
  for(let i=0;i<40;i++)a.send(JSON.stringify({content:'spam'+i})); await wait(800);
  const got=b.msgs.filter(m=>m.type==='chat').length; ok(got>=10&&got<=13,`flood of 40 -> ${got} delivered (burst 10 + refill)`);
  ok(a.msgs.filter(m=>m.type==='error').length===1,'spammer gets exactly one "too fast" notice');
  ok(b.msgs.every(m=>m.type!=='error'),'other members never see the notice');
  await wait(1100); a.send(JSON.stringify({content:'calm again'})); await wait(300); ok(b.msgs.some(m=>m.content==='calm again'),'normal pace works again after a pause');
  a.close();b.close();

  // uploads: count 30/10min
  const r3=await newRoom('198.51.100.8'); const u=await conn(r3,'198.51.100.9'); const tok=u.msgs[0].token;
  const us=[];for(let i=0;i<31;i++)us.push((await up(r3,tok,'198.51.100.9','x','f'+i)).status);
  ok(us.slice(0,30).every(s=>s===200)&&us[30]===429,'31st upload in 10 min -> 429');
  u.close();
  // upload bytes: 200MB/hour across rooms (room quota is 100MB, so use 3 rooms)
  const big=new Uint8Array(10*1024*1024); const bs=[];
  for(const n of [5,5,1]){const r=await newRoom('198.51.100.10');const w=await conn(r,'198.51.100.11');for(let i=0;i<n;i++)bs.push((await up(r,w.msgs[0].token,'198.51.100.11',big,'b.bin')).status);w.close();}
  ok(bs.slice(0,10).every(s=>s===200)&&bs[10]===429,`upload volume: 10x10MB ok, 11th (over 100MB/h) -> ${bs[10]}`);
  const ur=await (await (async()=>{const r=await newRoom('198.51.100.12');const w=await conn(r,'198.51.100.11');const x=await up(r,w.msgs[0].token,'198.51.100.11',big,'c.bin');w.close();return x})()).json();
  ok(/try again/.test(ur.message||''),'byte-limit refusal explains when to retry');
  // daily cap (300MB): simulate 295MB already uploaded today by this client
  execSync(`${process.env.REDIS_CLI} set rl:upload-bytes-day:198.51.100.15 ${295*1024*1024} px 86400000`);
  {const r=await newRoom('198.51.100.15');const w=await conn(r,'198.51.100.15');
   const small=await up(r,w.msgs[0].token,'198.51.100.15',new Uint8Array(4*1024*1024),'s.bin'); const over=await up(r,w.msgs[0].token,'198.51.100.15',new Uint8Array(4*1024*1024),'t.bin'); w.close();
   ok(small.status===200&&over.status===429,`daily upload cap: 295+4MB ok, +4MB more (over 300MB/day) -> ${over.status}`);}

  // secrets: 20 creates / 10 min; reveal 30/min
  const sec=JSON.stringify({ciphertext:'AAAA',iv:'AAAAAAAAAAAAAAAA',salt:'AAAAAAAAAAAAAAAAAAAAAA==',iterations:600000,auth:'A'.repeat(43)+'=',ttl:'1d'});
  const ss=[];for(let i=0;i<21;i++)ss.push((await post('/api/secrets','198.51.100.13',sec)).status);
  ok(ss.slice(0,20).every(s=>s===200)&&ss[20]===429,'21st secret in 10 min -> 429');
  const rv=[];for(let i=0;i<31;i++)rv.push((await post('/api/secrets/AAAAAAAAAAAAAAAAAAAAAA/reveal','198.51.100.14',JSON.stringify({auth:'A'.repeat(43)+'='}))).status);
  ok(rv.slice(0,30).every(s=>s===404)&&rv[30]===429,'31st reveal attempt in a minute -> 429');
}
if(mode==='untrusted'){
  // no trusted proxies: X-Forwarded-For must be ignored
  const st=[];for(let i=0;i<11;i++)st.push((await post('/api/rooms',`203.0.113.${i+1}`)).status);
  ok(st.slice(0,10).every(s=>s===200)&&st[10]===429,'faking a new X-Forwarded-For per request does NOT dodge the limit');
}
if(mode==='storagefull'){
  ok((await post('/api/rooms')).status===503,'storage full: new room -> 503');
  const sec=JSON.stringify({ciphertext:'AAAA',iv:'AAAAAAAAAAAAAAAA',salt:'AAAAAAAAAAAAAAAAAAAAAA==',iterations:600000,auth:'A'.repeat(43)+'=',ttl:'1d'});
  const r=await post('/api/secrets',null,sec); ok(r.status===503&&/capacity/.test((await r.json()).message),'storage full: new secret -> 503 with message');
}
if(mode==='filesfull'){
  const room=await newRoom(); ok(!!room,'file storage full: rooms can still be created');
  const w=await conn(room,null,'Alice');
  ok((await up(room,w.msgs[0].token,null,new Uint8Array(1536<<10),'big.bin')).status===200,'file storage: upload below the limit is accepted');
  const r=await up(room,w.msgs[0].token,null,'x','a.txt');
  ok(r.status===503&&/chat still works/.test((await r.json()).message),'file storage full: upload -> 503 with message');
  const sec=JSON.stringify({ciphertext:'AAAA',iv:'AAAAAAAAAAAAAAAA',salt:'AAAAAAAAAAAAAAAAAAAAAA==',iterations:600000,auth:'A'.repeat(43)+'=',ttl:'1d'});
  ok((await post('/api/secrets',null,sec)).status===200,'file storage full: secrets still work');
  const b=await conn(room,null,'Bob'); w.send(JSON.stringify({content:'still chatting'})); await wait(300);
  ok(b.msgs.some(m=>m.content==='still chatting'),'file storage full: chat still works'); w.close(); b.close();
}
if(mode==='disabled'){
  const st=[];for(let i=0;i<15;i++)st.push((await post('/api/rooms')).status); ok(st.every(s=>s===200),'RATE_LIMIT=false: 15 rooms/min all allowed');
}
