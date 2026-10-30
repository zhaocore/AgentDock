import { chromium, type BrowserContext } from 'playwright';
import { parseHTML } from 'linkedom';
import { toMarkdown } from './main';
import { request, type RequestOptions } from 'node:https';
import type { ClientRequest, IncomingMessage } from 'node:http';
import { createBrotliDecompress, createGunzip, createInflate } from 'node:zlib';

type Input = { url: string; origin: string; cookie: string; ready_selector: string };
type Failure = { code: string; status: number; retryable: boolean };
type RequestResponse = (url: string, options: RequestOptions, listener: (response: IncomingMessage) => void) => ClientRequest;

/** 采集错误仅包含受控代码，不输出浏览器调用日志或凭据。 */
export class CaptureError extends Error {
  constructor(readonly failure: Failure) { super(failure.code); }
}

/** 校验内存协议，不把外部输入直接交给浏览器。 */
export function parseInput(raw: string): Input {
  const data: object = JSON.parse(raw);
  for (const key of ['url', 'origin', 'cookie', 'ready_selector'] as const) {
    if (!data || typeof data !== 'object' || !(key in data) || typeof data[key as keyof typeof data] !== 'string') throw new Error('采集输入缺少必要字符串');
  }
  const input = data as Input;
  const url = new URL(input.url);
  if (url.protocol !== 'https:' || url.origin !== input.origin || url.username || url.password || url.hash || !input.cookie || !input.ready_selector) throw new Error('采集来源或登录配置非法');
  return input;
}

/** 转换正文，保留远程资源链接，不递归下载。 */
export function pageMarkdown(html: string, base: string): string {
  const { document } = parseHTML(html);
  for (const node of document.querySelectorAll('script,style,noscript,form,nav,header,footer')) node.remove();
  for (const node of document.querySelectorAll('[href],[src],[poster],[data]')) {
    for (const attribute of ['href','src','poster','data']) {
      const raw = node.getAttribute(attribute);
      if (raw && !raw.startsWith('#')) {
        const address = URL.parse(raw, base);
        if (address && ['https:','http:'].includes(address.protocol)) node.setAttribute(attribute,address.href);
        else node.removeAttribute(attribute);
      }
    }
  }
  const body = document.querySelector('main,article') ?? document.body;
  return toMarkdown(body.innerHTML).trim();
}

/** 每次采集独立上下文；路由禁用缓存，正文只经内存返回。 */
export async function captureInContext(input: Input, context: BrowserContext, requestResponse: RequestResponse) {
    await context.addCookies(input.cookie.split(';').map(pair => {
      const split=pair.indexOf('=');
      if(split<1) throw new CaptureError({code:'invalid_cookie',status:0,retryable:false});
      return {name:pair.slice(0,split).trim(),value:pair.slice(split+1).trim(),url:input.origin,secure:true,httpOnly:true};
    }));
    let failure: Failure | null = null;
    let bytes=0;
    const controller=new AbortController();
    let notifyFailure!: (value: Failure) => void;
    const failed=new Promise<Failure>(resolve=>{notifyFailure=resolve;});
    await context.routeWebSocket('**/*', socket=>socket.close());
    await context.route('**/*',async route=>{
      if(controller.signal.aborted){await route.abort();return;}
      const request=route.request();
      const url=new URL(request.url());
      if(url.origin!==input.origin || !['GET','HEAD'].includes(request.method())) {
        if(request.isNavigationRequest()) failure={code:'login_or_cross_origin_redirect',status:0,retryable:false};
        await route.abort();return;
      }
      try {
        // 暂停的请求可能尚无完整网络头，Cookie 从当前隔离上下文按 URL 读取。
        const requestHeaders=request.headers();
        const cookies=await context.cookies(request.url());
        if(cookies.length)requestHeaders.cookie=cookies.map(cookie=>`${cookie.name}=${cookie.value}`).join('; ');
        const response=await new Promise<IncomingMessage>((resolve,reject)=>{
          const outgoing=requestResponse(request.url(),{method:request.method(),headers:{...requestHeaders,'accept-encoding':'gzip, deflate, br'},signal:AbortSignal.any([controller.signal,AbortSignal.timeout(10000)])},resolve);
          outgoing.once('error',reject);
          outgoing.end();
        });
        const status=response.statusCode;
        if(status===undefined){response.destroy();throw new CaptureError({code:'invalid_response',status:0,retryable:false});}
        if(request.isNavigationRequest() && (status<200||status>=300)) {
          response.destroy();
          throw new CaptureError({code:status>=300&&status<400?'login_redirect':'http_error',status,retryable:status===429||status>=500});
        }
        if(Number(response.headers['content-length']??0)>4*1024*1024) {
          response.destroy();
          throw new CaptureError({code:'response_too_large',status,retryable:false});
        }
        const chunks: Buffer[]=[];
        let size=0;
        const encoding=request.method()==='HEAD'||status===204||status===304?undefined:response.headers['content-encoding'];
        if(encoding && !['gzip','deflate','br','identity'].includes(encoding)){response.destroy();throw new CaptureError({code:'unsupported_encoding',status,retryable:false});}
        const decoder=encoding==='gzip'?createGunzip():encoding==='deflate'?createInflate():encoding==='br'?createBrotliDecompress():null;
        const body=decoder?response.pipe(decoder):response;
        const destroyBody=(error: Error)=>body.destroy(error);
        response.on('error',destroyBody);
        try{
            for await(const chunk of body as AsyncIterable<Buffer>){
              size+=chunk.byteLength;bytes+=chunk.byteLength;
              if(size>4*1024*1024||bytes>16*1024*1024)throw new CaptureError({code:'response_too_large',status,retryable:false});
              chunks.push(chunk);
            }
        }finally{response.off('error',destroyBody);response.destroy();decoder?.destroy();}
        const headers: Record<string,string>={};
        for(const [name,value] of Object.entries(response.headers)){
          if(value!==undefined)headers[name]=Array.isArray(value)?value.join(name==='set-cookie'?'\n':', '):value;
        }
        // 返回解压后的流，不能把原压缩编码和传输长度交给浏览器。
        for(const header of ['content-encoding','content-length','transfer-encoding','connection'])delete headers[header];
        await route.fulfill({status,headers,body:Buffer.concat(chunks,size)});
      } catch(error) {
        failure??=error instanceof CaptureError?error.failure:{code:'network_error',status:0,retryable:true};
        notifyFailure(failure);
        controller.abort();
        await route.abort();
      }
    });
    const page=await context.newPage();
    try {
      await page.goto(input.url,{waitUntil:'domcontentloaded',timeout:30000});
      await Promise.race([page.locator(input.ready_selector).first().waitFor({state:'visible',timeout:10000}),failed.then(value=>{throw new CaptureError(value);})]);
      if(failure) throw new CaptureError(failure);
      if(new URL(page.url()).origin!==input.origin)throw new CaptureError({code:'login_redirect',status:0,retryable:false});
      const html=await page.content();
      if(Buffer.byteLength(html)>4*1024*1024) throw new CaptureError({code:'response_too_large',status:0,retryable:false});
      const markdown=pageMarkdown(html,page.url());
      const title=(await page.title()).trim();
      if(!title||title.length>500||!markdown||Array.from(markdown).length>24000)throw new CaptureError({code:'empty_or_oversized_page',status:0,retryable:false});
      return {url:input.url,title,captured_at:new Date().toISOString(),markdown};
    } catch(error) {
      if(failure)throw new CaptureError(failure);
      if(error instanceof CaptureError)throw error;
      throw new CaptureError({code:'login_marker_or_page_timeout',status:0,retryable:false});
    } finally {controller.abort();}
}

/** 生产入口严格验证 TLS，每次销毁浏览器登录上下文。 */
export async function capturePage(input: Input) {
 const browser=await chromium.launch({args:['--disk-cache-size=0','--disable-background-networking']});
 try{return await captureInContext(input,await browser.newContext({acceptDownloads:false,serviceWorkers:'block'}),request);}
 finally{await browser.close();}
}

if(import.meta.main){
  let result: object;
  try{result={page:await capturePage(parseInput(await Bun.stdin.text()))};}
  catch(error){result={error:error instanceof CaptureError?error.failure:{code:'capture_configuration_or_browser_error',status:0,retryable:false}};}
  await new Promise<void>((resolve,reject)=>process.stdout.write(JSON.stringify(result)+'\n',error=>error?reject(error):resolve()));
}
