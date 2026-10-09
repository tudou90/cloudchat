// Security headers, health check, and HTTPS-only behaviour (PUBLIC_URL=https://…).
const B=process.env.BASE_URL, https=(process.env.PUBLIC_URL||'').startsWith('https://');
const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+`[https=${https}] `+m); if(!c) process.exitCode=1};
for (const path of ['/', '/chat/', '/chat/secret', '/changelog']) {
  const h=(await fetch(B+path)).headers;
  ok(h.get('content-security-policy')?.includes("frame-ancestors 'none'") && h.get('x-frame-options')==='DENY' && h.get('x-content-type-options')==='nosniff',`${path}: CSP, X-Frame-Options, nosniff`);
  ok((h.get('strict-transport-security')!==null)===https,`${path}: HSTS ${https?'on':'off'}`);
}
const hz=await fetch(B+'/healthz'); ok(hz.status===200&&(await hz.json()).status==='ok','/healthz reports ok');
const cookie=(await fetch(B+'/api/rooms',{method:'POST'})).headers.get('set-cookie')||'';
ok(/cc_identity=/.test(cookie)&&/HttpOnly/i.test(cookie),'identity cookie is HttpOnly');
ok(/;\s*Secure/i.test(cookie)===https,`identity cookie Secure flag ${https?'set':'not set'}`);
