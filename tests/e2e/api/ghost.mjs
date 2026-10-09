import { execSync, spawn } from 'node:child_process';
const B=process.env.BASE_URL;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const rc=a=>execSync(`${process.env.REDIS_CLI} ${a}`).toString().trim();
// This suite starts, stops and kills its own server.
const start=()=>{const p=spawn(process.env.SERVER_BIN,[],{cwd:process.env.REPO_ROOT,env:{...process.env,SERVER_ADDR:':'+new URL(B).port},stdio:['ignore','pipe','pipe']});p.log='';p.stderr.on('data',d=>p.log+=d);return wait(1200).then(()=>p)};
const conn=(room,name)=>new Promise((res,rej)=>{const ws=new WebSocket(`${process.env.BASE_URL.replace(/^http/,'ws')}/ws/${room}?name=${name}`);ws.msgs=[];ws.closed=false;ws.onclose=()=>ws.closed=true;
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>res(ws),300);ws.onerror=rej;});
const names=ws=>ws.msgs.filter(m=>m.type==='presence').at(-1)?.members.map(m=>m.name).sort().join(',');

let srv=await start();
const room=(await (await fetch(`${B}/api/rooms`,{method:'POST'})).json()).id;
// 1) graceful shutdown
let a=await conn(room,'Alice'), b=await conn(room,'Bob');
ok(+rc(`hlen room:${room}:members`)===2,'2 members registered');
srv.kill('SIGINT'); await wait(1500);
ok(a.closed&&b.closed,'SIGINT: clients get disconnected');
ok(+rc(`hlen room:${room}:members`)===0,'SIGINT: members removed from Redis');
const pt=+rc(`pttl room:${room}:meta`); ok(pt>0&&pt<=300000,`SIGINT: empty room enters 5-min countdown (pttl ${pt})`);
ok(/Disconnected 2 client/.test(srv.log),'shutdown log reports 2 clients');

// 2) crash (SIGKILL) leaves ghosts; heartbeat sweeps them
srv=await start();
a=await conn(room,'Alice'); b=await conn(room,'Bob');
srv.kill('SIGKILL'); await wait(500);
ok(+rc(`hlen room:${room}:members`)===2,'SIGKILL: 2 ghost members left behind');
srv=await start();
a=await conn(room,'Alice'); await wait(300);
console.log('   presence right after rejoin:',names(a),'(ghosts not yet stale)');
const t0=Date.now(); while(names(a)!=='Alice'&&Date.now()-t0<75000) await wait(1000);
ok(names(a)==='Alice',`heartbeat sweeps ghosts -> "1 online" pushed after ${Math.round((Date.now()-t0)/1000)}s`);
ok(+rc(`hlen room:${room}:members`)===1&&+rc(`zcard room:${room}:seen`)===1,'Redis members/seen hold only the live connection');

// 3) legacy entry without heartbeat (pre-JSON format) is dropped on next join
rc(`hset room:${room}:members legacy-token 阿杜`);
b=await conn(room,'Bob'); await wait(300);
ok(rc(`hexists room:${room}:members legacy-token`)==='0','legacy member entry swept on join');
ok(names(a)==='Alice,Bob','presence shows only real people');
b.close(); await wait(400); ok(names(a)==='Alice','Bob exits -> 1 online');
a.close(); await wait(400);
srv.kill('SIGINT'); await wait(800);
