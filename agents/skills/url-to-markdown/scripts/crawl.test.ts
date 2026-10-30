import {test,expect} from 'bun:test';
import {mkdtemp,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {chromium} from 'playwright';
import {captureInContext,pageMarkdown,parseInput} from './crawl';
import https, {request} from 'node:https';
import {randomBytes} from 'node:crypto';
import {createGzip} from 'node:zlib';

test('后台转换保留远程链接并剔除脚本及导航',()=>{
 const md=pageMarkdown('<html><body><nav>noise</nav><main><h1>Title</h1><p>Fact</p><img src="/image.png"><a href="/source">source</a><script>secret()</script></main></body></html>','https://docs.example/page');
 expect(md).toContain('https://docs.example/image.png');expect(md).toContain('https://docs.example/source');expect(md).not.toContain('secret');expect(md).not.toContain('noise');
 expect(()=>parseInput('{}')).toThrow();expect(()=>parseInput('{"url":"http://docs.example","origin":"http://docs.example","cookie":"x=y","ready_selector":"main"}')).toThrow();
});

test('来源规范与 Go 配置一致，支持非默认端口和压缩 IPv6',()=>{
 for(const origin of ['https://docs.example','https://docs.example:8443','https://[::1]','https://[2001:db8::1]:8443']){
  expect(parseInput(JSON.stringify({url:origin+'/page',origin,cookie:'session=test',ready_selector:'main'})).origin).toBe(origin);
 }
 for(const origin of ['https://DOCS.example','https://docs.example:443','https://[0:0:0:0:0:0:0:1]','https://例子.example']){
  expect(()=>parseInput(JSON.stringify({url:origin+'/page',origin,cookie:'session=test',ready_selector:'main'}))).toThrow();
 }
});

test('声明长度、无长度、压缩和并行累计超限均提前断流',{timeout:60000},async()=>{
 const dir=await mkdtemp(path.join(tmpdir(),'crawl-stream-test-'));
 execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-keyout',path.join(dir,'key.pem'),'-out',path.join(dir,'cert.pem'),'-days','1','-subj','/CN=localhost'],{stdio:'ignore'});
 const sent=new Map<string,number>();
 const total=8*1024*1024;
 const server=https.createServer({key:await readFile(path.join(dir,'key.pem')),cert:await readFile(path.join(dir,'cert.pem'))},(request,response)=>{
  const url=new URL(request.url!,'https://localhost');
  if(url.pathname==='/aggregate'){
   response.writeHead(200,{'content-type':'text/html'});
   response.end(`<html><head><title>Streams</title></head><body><main>Fact</main><script>Promise.all(Array.from({length:12},(_,i)=>fetch('/resource?i='+i))).then(()=>document.querySelector('main').dataset.ready='yes')</script></body></html>`);
   return;
  }
  const mode=url.pathname.slice(1);
  const length=mode==='resource'?3*1024*1024:total;
  const headers: Record<string,string>={'content-type':'application/octet-stream'};
  if(mode==='declared')headers['content-length']=String(length);
  if(mode==='compressed')headers['content-encoding']='gzip';
  response.writeHead(200,headers);response.flushHeaders();
  const gzip=mode==='compressed'?createGzip():null;
  if(gzip)gzip.pipe(response);
  const output=gzip??response;
  let size=0;
  const timer=setInterval(()=>{
   const chunk=randomBytes(Math.min(16384,length-size));
   output.write(chunk);size+=chunk.length;sent.set(mode,(sent.get(mode)??0)+chunk.length);
   if(size===length){clearInterval(timer);output.end();}
  },3);
  response.on('close',()=>{clearInterval(timer);gzip?.destroy();});
 });
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));
 const address=server.address();
 if(!address||typeof address==='string')throw new Error('测试 TLS 服务地址缺失');
 const origin=`https://127.0.0.1:${address.port}`;
 const browser=await chromium.launch();
 try{
  for(const mode of ['declared','chunked','compressed','aggregate']){
   const context=await browser.newContext({ignoreHTTPSErrors:true,serviceWorkers:'block',acceptDownloads:false});
   try{
    await expect(captureInContext({url:origin+'/'+mode,origin,cookie:'session=test',ready_selector:'[data-ready=yes]'},context,(url,options,handler)=>request(url,{...options,rejectUnauthorized:false},handler))).rejects.toThrow('response_too_large');
    const received=sent.get(mode==='aggregate'?'resource':mode)??0;
    expect(received).toBeLessThan(mode==='aggregate'?36*1024*1024:total);
    if(mode==='declared')expect(received).toBeLessThan(1024*1024);
    console.log(JSON.stringify({event:'bounded_crawl_stream',mode,sent_bytes:received}));
   }finally{await context.close();}
  }
 }finally{await browser.close();server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()));await rm(dir,{recursive:true});}
});

test('真实浏览器校验 Cookie、正文、鉴权失败和重定向',{timeout:60000},async()=>{
 const dir=await mkdtemp(path.join(tmpdir(),'crawl-browser-test-'));
 execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-keyout',path.join(dir,'key.pem'),'-out',path.join(dir,'cert.pem'),'-days','1','-subj','/CN=localhost'],{stdio:'ignore'});
 const server=https.createServer({key:await readFile(path.join(dir,'key.pem')),cert:await readFile(path.join(dir,'cert.pem'))},(request,response)=>{
  const route=new URL(request.url!,'https://localhost').pathname;
  if(route==='/redirect'){response.writeHead(302,{location:'/login'});response.end();return;}
  if(route==='/head'){response.writeHead(200,{'content-encoding':'gzip'});response.end();return;}
  const cookie=request.headers.cookie??'';
  if(route==='/check-cookies'){
   response.writeHead(['session=test','second=value','third=value'].every(value=>cookie.includes(value))?200:401);response.end('checked');return;
  }
  if(cookie!=='session=test'){response.writeHead(401);response.end('Unauthorized');return;}
  response.writeHead(200,{'content-type':'text/html','set-cookie':['second=value; Path=/; Secure; HttpOnly','third=value; Path=/; Secure; HttpOnly']});
  response.end('<html><head><title>Controlled Page</title></head><body><main><h1>Facts</h1><p>Verified fact</p><img src="https://outside.invalid/pixel"></main><script>Promise.all([fetch("/check-cookies"),fetch("/head",{method:"HEAD"})]).then(responses=>{if(responses.every(response=>response.ok))document.querySelector("main").dataset.ready="yes"})</script></body></html>');
 });
 await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));
 const address=server.address();
 if(!address||typeof address==='string')throw new Error('测试 TLS 服务地址缺失');
 const browser=await chromium.launch();
 try{
  const origin=`https://127.0.0.1:${address.port}`;
  for(const mode of ['good','auth','redirect']){
   const context=await browser.newContext({ignoreHTTPSErrors:true,serviceWorkers:'block',acceptDownloads:false});
   try{const input=parseInput(JSON.stringify({url:origin+'/'+mode,origin,cookie:mode==='auth'?'session=expired':'session=test',ready_selector:'[data-ready="yes"]'}));
    if(mode==='good'){const page=await captureInContext(input,context,(url,options,handler)=>request(url,{...options,rejectUnauthorized:false},handler));expect(page.markdown).toContain('Verified fact');expect(page.markdown).toContain('outside.invalid/pixel');}
    else {await expect(captureInContext(input,context,(url,options,handler)=>request(url,{...options,rejectUnauthorized:false},handler))).rejects.toThrow(mode==='auth'?'http_error':'login_redirect');}
   }finally{await context.close();}
  }
 }finally{await browser.close();server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()));await rm(dir,{recursive:true});}
});
