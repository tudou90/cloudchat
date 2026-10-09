import { execSync } from 'node:child_process';
const B=process.env.BASE_URL; const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms)); const rc=a=>execSync(`${process.env.REDIS_CLI} ${a}`).toString().trim();
const conn=(room,name)=>new Promise(res=>{const ws=new WebSocket(`${process.env.BASE_URL.replace(/^http/,'ws')}/ws/${room}?name=${name}`);ws.msgs=[];ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>res(ws),300)});
const names=ws=>ws.msgs.filter(m=>m.type==='presence').at(-1)?.members.map(m=>m.name).sort().join(',');
const room=(await (await fetch(B+'/api/rooms',{method:'POST'})).json()).id;
const a=await conn(room,'Alice'), b=await conn(room,'Bob'); await wait(200);
a.send(JSON.stringify({content:'one'})); await wait(150); a.send(JSON.stringify({content:'two'})); await wait(300);
const seqs=b.msgs.filter(m=>m.type==='chat').map(m=>m.seq); ok(seqs.length===2&&seqs[1]===seqs[0]+1,'messages carry consecutive seq numbers '+seqs);
rc(`del room:${room}:members room:${room}:seen`);  // simulate everyone being wrongly swept (e.g. clock jump)
const before=a.msgs.length, t0=Date.now();
while(names(a)!=='Alice,Bob'||a.msgs.length===before){ if(Date.now()-t0>20000)break; await wait(500);} 
ok(+rc(`hlen room:${room}:members`)===2,`heartbeat restored both live members (after ${Math.round((Date.now()-t0)/1000)}s)`);
ok(names(a)==='Alice,Bob'&&a.msgs.length>before,'restored presence was pushed to clients');
const c=await conn(room,'Carol'); ok(!c.msgs.find(m=>m.type==='history'),'late joiner still gets no earlier history (seq-based)');
a.send(JSON.stringify({content:'three'})); await wait(300); c.close(); await wait(300);
a.close();b.close();
