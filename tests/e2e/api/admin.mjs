// `cloudchat admin` moderation commands: show, export, delete rooms and secrets.
import { execFileSync, execSync } from 'node:child_process';
import { mkdtempSync, readFileSync, readdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
const B=process.env.BASE_URL, W=B.replace(/^http/,'ws');
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
// Run from tests/e2e so no developer .env is picked up; Redis settings come from env.
const admin=(args,input='')=>{try{return {code:0,out:execFileSync(process.env.SERVER_BIN,['admin',...args],{input,env:process.env,stdio:['pipe','pipe','pipe']}).toString()}}catch(e){return {code:e.status,out:String(e.stdout)+String(e.stderr)}}};
const keys=room=>execSync(`${process.env.REDIS_CLI} --scan --pattern 'room:${room}*'`).toString().split('\n').filter(Boolean);
const conn=(room,name)=>new Promise(res=>{const ws=new WebSocket(`${W}/ws/${room}?name=${name}`);ws.msgs=[];ws.closed=false;ws.onclose=()=>ws.closed=true;
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>res(ws),300)});

const room=(await (await fetch(B+'/api/rooms',{method:'POST'})).json()).id, link=`${B}/chat/?room=${room}`;
const a=await conn(room,'Alice'), b=await conn(room,'Bob');
a.send(JSON.stringify({content:'reported message'})); await wait(200);
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==','base64');
const f=new FormData(); f.append('file',new Blob([png]),'evidence.png');
await fetch(`${B}/api/rooms/${room}/files`,{method:'POST',body:f,headers:{'X-Client-Token':a.msgs[0].token}}); await wait(300);

let r=admin(['room','show',link]);
ok(r.code===0&&/Online now:\s+2/.test(r.out)&&/Alice/.test(r.out)&&/Bob/.test(r.out)&&/Messages:\s+2 kept/.test(r.out)&&/evidence\.png/.test(r.out),'room show (from invite link): members, messages, files');
ok(admin(['room','show',room]).code===0,'room show accepts a bare room ID');
ok(admin(['room','show','not-a-room']).code!==0,'invalid room reference is rejected');

r=admin(['room','list']); const row=r.out.split('\n').find(l=>l.startsWith(room))||'';
ok(r.code===0&&/1 room\(s\) \(unlimited\), 2 person\(s\) online — times in/.test(r.out)&&/^\S+\s+\d{4}-\d\d-\d\d \d\d:\d\d\s+\d+s\s+2\s+2\s+1 \(1 KB\)\s+\S+$/.test(row),'room list: every room with age, online, messages, files');
const dir=mkdtempSync(join(tmpdir(),'cc-export-')); r=admin(['room','export',link,dir]);
const sub=readdirSync(dir)[0]; const rec=JSON.parse(readFileSync(join(dir,sub,'room.json'),'utf8')); const files=readdirSync(join(dir,sub,'files'));
ok(r.code===0&&rec.messages.some(m=>m.content==='reported message')&&rec.files.length===1,'export writes room.json with messages and file list');
ok(files.length===1&&readFileSync(join(dir,sub,'files',files[0])).equals(png),'export writes the original file bytes');

r=admin(['room','delete',link],'no\n'); ok(r.code!==0&&keys(room).length>0,'delete without confirmation is aborted, nothing removed');
r=admin(['room','delete',link,'--yes']); await wait(600);
ok(r.code===0&&/Deleted room .* \(1 file\(s\)\)/.test(r.out),'delete --yes reports what was removed');
ok(keys(room).length===0,'no Redis keys left for the room (messages, files, members)');
ok(a.msgs.some(m=>m.type==='closed'&&/violating our Terms/.test(m.content))&&b.msgs.some(m=>m.type==='closed'),'everyone in the room is told it was closed');
ok(a.closed&&b.closed,'their connections are closed');
ok((await fetch(`${B}/api/rooms/${room}`)).status===404,'invite link no longer works');
await wait(16000); ok(keys(room).length===0,'heartbeat does not recreate the deleted room');

const sec=JSON.stringify({ciphertext:'AAAA',iv:'AAAAAAAAAAAAAAAA',salt:'AAAAAAAAAAAAAAAAAAAAAA==',iterations:600000,auth:'A'.repeat(43)+'=',ttl:'7d'});
const sid=(await (await fetch(B+'/api/secrets',{method:'POST',headers:{'content-type':'application/json'},body:sec})).json()).id;
r=admin(['secret','show',`${B}/chat/secret/${sid}#somekey`]); ok(r.code===0&&/exists, expires in/.test(r.out),'secret show (from note link with #key)');
r=admin(['secret','delete',sid,'--yes']); ok(r.code===0&&(await fetch(`${B}/api/secrets/${sid}`)).status===404,'secret delete removes the note');
