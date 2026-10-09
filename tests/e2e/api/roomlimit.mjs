// MAX_ROOMS caps rooms platform-wide; rooms nobody joins expire like empty ones.
import { execFileSync } from 'node:child_process';
const B=process.env.BASE_URL, W=B.replace(/^http/,'ws');
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const admin=(...args)=>execFileSync(process.env.SERVER_BIN,['admin',...args],{env:{...process.env,MAX_ROOMS:'3'}}).toString();
const create=()=>fetch(B+'/api/rooms',{method:'POST'});
const conn=room=>new Promise(res=>{const ws=new WebSocket(`${W}/ws/${room}?name=Alice`);ws.onopen=()=>setTimeout(()=>res(ws),300)});

const ids=[]; for(let i=0;i<3;i++){const r=await create(); ok(r.status===200,`room ${i+1} of 3 is created`); ids.push((await r.json()).id)}
let r=await create(), body=await r.json();
ok(r.status===503&&/maximum number of active chat rooms/.test(body.message)&&r.headers.get('retry-after'),'room 4 is refused with 503 and a clear message');
ok(/3 room\(s\) \(limit 3\)/.test(admin('room','list')),'admin room list shows the count and the limit');

admin('room','delete',ids[2],'--yes');
r=await create(); ok(r.status===200,'deleting a room frees a slot'); ids[2]=(await r.json()).id;
ok((await create()).status===503,'and the limit applies again');

const ws=await conn(ids[0]);
await wait(3000);
ok((await fetch(`${B}/api/rooms/${ids[0]}`)).status===200,'a joined room outlives EMPTY_ROOM_TTL');
ok((await fetch(`${B}/api/rooms/${ids[1]}`)).status===404,'a room nobody joined expires after EMPTY_ROOM_TTL');
ok((await create()).status===200&&(await create()).status===200,'expired rooms free their slots');
ok((await create()).status===503,'joined room still counts toward the limit');
ws.close();
