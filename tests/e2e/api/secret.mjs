// Test the real browser crypto module. frontend/ is a CommonJS package, so
// strip the TypeScript types and load the source as an ES module.
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
const cryptoSrc = readFileSync(new URL('../../../frontend/src/utils/secretCrypto.ts', import.meta.url), 'utf8');
const { encryptSecret, prepareReveal, decryptSecret } = await import('data:text/javascript,' + encodeURIComponent(stripTypeScriptTypes(cryptoSrc)));
import { execSync } from 'node:child_process';
const B=process.env.BASE_URL;
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m); if(!c) process.exitCode=1};
const post=(u,b)=>fetch(B+u,{method:'POST',headers:{'content-type':'application/json'},body:typeof b==='string'?b:JSON.stringify(b)});
const make=async(text,pw,ttl='1d')=>{const {payload,linkKey}=await encryptSecret(text,pw);const r=await post('/api/secrets',{...payload,ttl});return {r,linkKey,payload,j:await r.json()}};
const tryReveal=async(id,pw,linkKey)=>{const m=await (await fetch(`${B}/api/secrets/${id}`)).json();const k=await prepareReveal(pw,m.salt,m.iterations,linkKey);const r=await post(`/api/secrets/${id}/reveal`,{auth:k.auth});return {r,k,j:await r.json()}};

const text='机密内容 🔐 line1\nline2';
const {r,linkKey,j}=await make(text,'p@ss');
ok(r.status===200&&j.id&&j.expiresAt,'create secret');
const meta=await (await fetch(`${B}/api/secrets/${j.id}`)).json();
ok(meta.attemptsLeft===5&&meta.iterations===600000,'meta: 5 attempts, iterations');
const ttl=+execSync(`${process.env.REDIS_CLI} ttl secret:${j.id}`).toString(); ok(ttl>86390&&ttl<=86400,'redis TTL ~1d');
const dump=execSync(`${process.env.REDIS_CLI} hgetall secret:${j.id}`).toString();
ok(!dump.includes('机密')&&!dump.includes('p@ss')&&!dump.includes(linkKey),'redis holds no plaintext/password/link key');
let x=await tryReveal(j.id,'wrong',linkKey); ok(x.r.status===403&&x.j.attemptsLeft===4,'wrong password -> 403, 4 left');
x=await tryReveal(j.id,'p@ss',linkKey.slice(0,-2)+(linkKey.endsWith('AA')?'BA':'AA')); ok(x.r.status===403,'right password + wrong link key -> 403');
x=await tryReveal(j.id,'p@ss',linkKey); ok(x.r.status===200&&await decryptSecret(x.k.encKey,x.j.ciphertext,x.j.iv)===text,'correct -> decrypts to original');
ok(x.r.headers.get('cache-control')==='no-store','reveal response no-store');
ok((await fetch(`${B}/api/secrets/${j.id}`)).status===404,'after read: meta 404');
ok((await post(`/api/secrets/${j.id}/reveal`,{auth:x.k.auth})).status===404,'after read: reveal 404');

const s2=await make('x','pw');
const st=[];for(let i=0;i<5;i++) st.push((await tryReveal(s2.j.id,'bad'+i,s2.linkKey)).r.status);
ok(st.join()==='403,403,403,403,410','5 wrong attempts -> destroyed (410): '+st);
ok((await fetch(`${B}/api/secrets/${s2.j.id}`)).status===404,'destroyed secret gone');

const s3=await make('race','pw','30d');
const m3=await (await fetch(`${B}/api/secrets/${s3.j.id}`)).json(); const k3=await prepareReveal('pw',m3.salt,m3.iterations,s3.linkKey);
const codes=(await Promise.all(Array.from({length:10},()=>post(`/api/secrets/${s3.j.id}/reveal`,{auth:k3.auth})))).map(r=>r.status);
ok(codes.filter(c=>c===200).length===1,'10 concurrent reveals -> exactly one succeeds');

const bad=async(patch,m)=>{const {payload}=await encryptSecret('v','pw');const r=await post('/api/secrets',{...payload,ttl:'1d',...patch});ok(r.status===400,m+' -> 400 '+JSON.stringify(await r.json()))};
await bad({ttl:'2d'},'bad ttl'); await bad({iterations:1000},'low iterations'); await bad({iv:'AAAA'},'bad iv'); await bad({auth:''},'missing auth');
const big=await post('/api/secrets',{ciphertext:'A'.repeat(200000),iv:'',salt:'',iterations:600000,auth:'x',ttl:'1d'}); ok(big.status===400,'oversized body rejected');
ok((await fetch(`${B}/api/secrets/not-valid`)).status===404,'malformed id -> 404');

for (const [p,want] of [['/chat/secret',200],['/chat/secret/abc',200],['/chat/',200],['/nope',404]]) {
  const r=await fetch(B+p); const t=await r.text(); ok(r.status===want&&(want!==200||t.includes('id="app"')),`GET ${p} -> ${r.status}`);
}
