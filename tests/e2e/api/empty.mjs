import { execSync } from 'node:child_process';
const P1=process.env.BASE_URL, P2=process.env.BASE_URL2;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const rc=a=>execSync(`${process.env.REDIS_CLI} ${a}`).toString().trim();
const conn=(B,room,name)=>new Promise((res,rej)=>{const ws=new WebSocket(`${B.replace('http','ws')}/ws/${room}?name=${name}`);ws.msgs=[];
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>res(ws),250);ws.onerror=rej;});
const close=async ws=>{ws.close();await wait(300)};
const keys=room=>rc(`--scan --pattern 'room:${room}*'`).split('\n').filter(Boolean);
const maxPttl=room=>Math.max(...keys(room).map(k=>+rc(`pttl '${k}'`)));
const minPttl=room=>Math.min(...keys(room).map(k=>+rc(`pttl '${k}'`)));

const room=(await (await fetch(`${P1}/api/rooms`,{method:'POST'})).json()).id;
let a=await conn(P1,room,'Alice'); a.send(JSON.stringify({content:'hello'}));
const f=new FormData();f.append('file',new Blob(['data']),'f.txt');
const info=await (await fetch(`${P1}/api/rooms/${room}/files`,{method:'POST',body:f,headers:{'X-Client-Token':a.msgs[0].token}})).json(); await wait(200);
ok(keys(room).length>=5,'room has meta/history/members/files/file keys: '+keys(room).length);
let b=await conn(P1,room,'Bob'); await close(b);
ok(minPttl(room)>3600_000,'one of two members leaves -> TTLs untouched');
await close(a);
ok(maxPttl(room)<=2000&&maxPttl(room)>0,`last member leaves -> all keys (incl. file) expire within 2s (max pttl ${maxPttl(room)})`);
a=await conn(P1,room,'Alice');
ok(minPttl(room)>3600_000,'rejoin within grace -> full TTL restored on every key');
ok(a.msgs.find(m=>m.type==='history')?.messages.some(m=>m.content==='hello'),'history intact after rejoin within grace');
ok((await fetch(P1+info.url)).status===200,'file still downloadable after rejoin');

// multi-instance: Alice on P1, Bob on P2
b=await conn(P2,room,'Bob'); await close(a);
ok(minPttl(room)>3600_000,'Alice leaves server1 while Bob is on server2 -> room kept');
await close(b);
ok(maxPttl(room)<=2000,'Bob leaves server2 -> room now emptying');
await wait(2500);
ok(keys(room).length===0,'after grace: no keys left in Redis for this room');
ok((await fetch(`${P1}/api/rooms/${room}`)).status===404,'invite link now -> 404 (room gone)');
ok((await fetch(P1+info.url)).status===404,'file gone');
// creator who never joins: room still lives full TTL (unchanged behaviour)
const r2=(await (await fetch(`${P1}/api/rooms`,{method:'POST'})).json()).id;
ok(+rc(`pttl room:${r2}:meta`)>3600_000,'created-but-never-joined room keeps ROOM_TTL');
