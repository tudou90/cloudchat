// Files are stored on disk (FILE_STORAGE_DIR); Redis holds only metadata,
// and the janitor deletes files from disk once their metadata expires.
import { execSync } from 'node:child_process';
import { existsSync, readFileSync, statSync, writeFileSync, mkdirSync, utimesSync } from 'node:fs';
import { join } from 'node:path';
const B=process.env.BASE_URL, W=B.replace(/^http/,'ws'), DIR=process.env.FILE_STORAGE_DIR;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const wait=ms=>new Promise(r=>setTimeout(r,ms));
const rc=cmd=>execSync(`${process.env.REDIS_CLI} ${cmd}`).toString().trim();
const conn=room=>new Promise(res=>{const ws=new WebSocket(`${W}/ws/${room}?name=Alice`);ws.msgs=[];
  ws.onmessage=e=>e.data.split('\n').forEach(l=>ws.msgs.push(JSON.parse(l)));ws.onopen=()=>setTimeout(()=>res(ws),300)});
const up=(room,tok,bytes,name)=>{const f=new FormData();f.append('file',new Blob([bytes]),name);return fetch(`${B}/api/rooms/${room}/files`,{method:'POST',body:f,headers:{'X-Client-Token':tok}})};
const newRoom=async()=>(await (await fetch(B+'/api/rooms',{method:'POST'})).json()).id;

const room=await newRoom(), a=await conn(room), tok=a.msgs[0].token;
const big=Buffer.alloc(3<<20); for(let i=0;i<big.length;i++) big[i]=i*7%251;
const info=await (await up(room,tok,big,'big.bin')).json();
const path=join(DIR,room,info.id);
ok(existsSync(path)&&readFileSync(path).equals(big),'file content is stored on disk');
ok((statSync(path).mode&0o777)===0o600&&(statSync(join(DIR,room)).mode&0o777)===0o700,'file and room directory are private (0600/0700)');
ok(rc(`hkeys room:${room}:file:${info.id}`).split('\n').sort().join()==='mime,name,size','Redis keeps only name, type and size');

let r=await fetch(B+info.url); ok(r.status===200&&Buffer.from(await r.arrayBuffer()).equals(big),'download streams the full file');
r=await fetch(B+info.url,{headers:{Range:'bytes=100-199'}}); const part=Buffer.from(await r.arrayBuffer());
ok(r.status===206&&part.equals(big.subarray(100,200)),'range requests work (resumable downloads)');

rc(`del room:${room}:file:${info.id}`);
ok((await fetch(B+info.url)).status===404,'metadata gone -> download 404 at once');
await wait(3500); ok(!existsSync(path),'janitor deletes the file from disk');

// Leftovers with no metadata (e.g. after a crash) are removed too.
const orphanDir=join(DIR,room); mkdirSync(orphanDir,{recursive:true});
const orphan=join(orphanDir,'AAAAAAAAAAAAAAAAAAAAAA'), tmp=join(orphanDir,'.tmp-123');
for(const f of [orphan,tmp]){writeFileSync(f,'x'); const old=new Date(Date.now()-60_000); utimesSync(f,old,old)}
await wait(2500); ok(!existsSync(orphan)&&!existsSync(tmp),'orphaned and half-written files are removed');

// When everyone leaves, the room and its files expire (EMPTY_ROOM_TTL=2s).
const info2=await (await up(room,tok,'hello','h.txt')).json(); const path2=join(DIR,room,info2.id);
ok(existsSync(path2),'second file stored');
a.close(); await wait(6000);
ok(!existsSync(path2)&&!existsSync(join(DIR,room)),'room emptied and expired -> its files and directory are gone from disk');
