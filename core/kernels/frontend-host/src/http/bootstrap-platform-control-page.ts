import { randomBytes } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { setIndexSecurityHeaders } from "./security-headers";

export const bootstrapPlatformControlPath = "/bootstrap/platform-control";

export function serveBootstrapPlatformControlPage(request: IncomingMessage, response: ServerResponse, head: boolean): void {
  const nonce = randomBytes(18).toString("base64url");
  setIndexSecurityHeaders(response, nonce);
  response.statusCode = 200;
  if (head) {
    response.end();
    return;
  }
  response.end(document(nonce));
}

function document(nonce: string): string {
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>配置平台控制数据库 · VastPlan</title>
<style>
:root{color-scheme:light dark;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;background:#f5f7fa;color:#1f2937}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:32px;background:linear-gradient(145deg,#eef3ff,#f7f8fa 42%,#eef8f5)}
main{width:min(920px,100%);background:#fff;border:1px solid #e5e7eb;border-radius:16px;box-shadow:0 18px 60px #1f29371a;padding:30px 34px}
h1{margin:0;font-size:26px}.lead{margin:8px 0 24px;color:#6b7280}.state{display:none;margin:0 0 20px;padding:10px 12px;border-radius:8px;background:#eff6ff;color:#1d4ed8}.state.error{background:#fef2f2;color:#b91c1c}
fieldset{border:0;border-top:1px solid #e5e7eb;margin:18px 0 0;padding:20px 0 0}legend{padding:0 12px 0 0;font-weight:650}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px 24px}
label{display:grid;grid-template-columns:130px minmax(0,1fr);align-items:center;gap:12px;font-size:14px}label>span{text-align:right;white-space:nowrap}.required:before{content:"*";color:#dc2626;margin-right:4px}
input,select{width:100%;min-width:0;height:38px;border:1px solid #d1d5db;border-radius:7px;padding:0 11px;background:#fff;color:#111827;font:inherit}input:focus,select:focus{outline:2px solid #2563eb33;border-color:#2563eb}
.actions{display:flex;justify-content:flex-end;gap:10px;margin-top:26px}.actions button{height:38px;border:1px solid #d1d5db;border-radius:7px;padding:0 18px;background:#fff;color:#1f2937;font:inherit;cursor:pointer}.actions .primary{background:#2563eb;border-color:#2563eb;color:#fff}.actions button:disabled{opacity:.55;cursor:not-allowed}
.hint{grid-column:1/-1;color:#6b7280;font-size:13px;padding-left:142px;margin-top:-6px}.hidden{display:none!important}
@media(max-width:760px){body{padding:12px}main{padding:22px 18px}.grid{grid-template-columns:1fr}label{grid-template-columns:1fr;gap:6px}label>span{text-align:left}.hint{padding-left:0}}
@media(prefers-color-scheme:dark){:root{background:#111827;color:#e5e7eb}body{background:linear-gradient(145deg,#111827,#171717)}main{background:#1f1f1f;border-color:#374151}input,select,.actions button{background:#171717;color:#f3f4f6;border-color:#4b5563}.lead,.hint{color:#9ca3af}fieldset{border-color:#374151}}
</style></head><body><main>
<h1>配置平台控制数据库</h1><p class="lead">首次启动只运行最小可信服务。连接测试、Schema 初始化和 Profile 提交全部成功后，系统才会启动完整平台插件组合。</p>
<p id="state" class="state" role="status" aria-live="polite"></p>
<form id="form" autocomplete="off">
<fieldset><legend>连接标识</legend><div class="grid">
<label><span class="required">数据库类型</span><select name="providerId"><option value="postgresql">PostgreSQL</option><option value="mysql">MySQL</option></select></label>
<label><span class="required">地址与端口</span><input name="endpoint" placeholder="db.example.com:5432" required spellcheck="false"></label>
<label><span class="required">数据库</span><input name="database" required spellcheck="false"></label>
<label><span class="required">Schema</span><input name="schema" value="vastplan_platform" required spellcheck="false"></label>
</div></fieldset>
<fieldset><legend>连接安全</legend><div class="grid">
<label><span class="required">用户名</span><input name="username" required autocomplete="off" spellcheck="false"></label>
<label><span class="required">传输加密</span><select name="tlsMode"><option value="verify-full">完整校验（推荐）</option><option value="verify-ca">仅校验证书链</option><option value="disable">关闭</option></select></label>
<label id="serverNameRow"><span>证书服务器名称</span><input name="serverName" spellcheck="false"></label>
<label><span class="required">密码提供方式</span><select name="secretMode"><option value="direct">直接输入密码（推荐）</option><option value="external">外部密钥引用（高级）</option></select></label>
<label class="secret-direct"><span class="required">密码</span><input name="password" type="password" autocomplete="new-password" maxlength="65536"></label>
<label class="secret-external hidden"><span class="required">外部密钥类型</span><select name="externalKind"><option value="systemd-credential">systemd Credential</option><option value="owner-file">Owner-only 文件</option></select></label>
<label class="secret-systemd hidden"><span class="required">Credential 名称</span><input name="externalName" placeholder="vastplan-platform-database" spellcheck="false"></label>
<label class="secret-file hidden"><span class="required">密码文件</span><input name="externalPath" placeholder="/absolute/path/database-password" spellcheck="false"></label>
<p class="hint">直接输入的密码仅用于本次测试与初始化；可信宿主会自动生成受保护引用，不会把明文写入 Profile、数据库、日志或错误信息。</p>
</div></fieldset>
<div class="actions"><button id="test" type="button">测试连接</button><button id="commit" class="primary" type="submit">初始化并启用</button></div>
</form></main>
<script nonce="${nonce}">
const form=document.querySelector('#form'),state=document.querySelector('#state'),test=document.querySelector('#test'),commit=document.querySelector('#commit');let status={phase:'unconfigured',generation:0},csrf='';
const message=(text,error=false)=>{state.textContent=text;state.className='state'+(error?' error':'');state.style.display=text?'block':'none'};
const busy=value=>{for(const element of form.elements)element.disabled=value};
const json=async(url,options={})=>{const response=await fetch(url,{credentials:'same-origin',...options,headers:{'Content-Type':'application/json',...(options.headers||{})}});let body={};try{body=await response.json()}catch{}if(response.status===401){location.href='/auth/login?returnTo='+encodeURIComponent(location.pathname);throw new Error('会话已失效，请重新登录')}if(!response.ok)throw new Error(friendly(body.error||'request_failed'));return body};
const friendly=code=>({platform_control_invalid:'配置格式无效，请检查各字段',platform_control_secret_unavailable:'无法读取密码来源，请检查文件权限或 Credential 名称',platform_control_database_unavailable:'数据库不可连接，请检查地址、TLS、账号和密码',platform_control_initialization_failed:'数据库初始化失败，请检查 Schema 权限',platform_control_conflict:'配置已被其他节点更新，请刷新后重试',csrf_rejected:'安全令牌已失效，请重试',platform_control_ready:'平台控制数据库已经启用，请进入平台管理中心',platform_service_unavailable:'数据库 Bootstrap 服务暂时不可用'})[code]||code;
const token=async()=>{if(csrf)return csrf;csrf=(await json('/v1/csrf')).token;return csrf};
const payload=()=>{const data=new FormData(form),mode=data.get('secretMode'),kind=data.get('externalKind'),tls=data.get('tlsMode');return{profile:{schemaVersion:1,generation:Number(status.generation||0)+1,providerId:data.get('providerId'),endpoint:String(data.get('endpoint')||'').trim(),database:String(data.get('database')||'').trim(),schema:String(data.get('schema')||'').trim(),tls:{mode:tls,...(tls==='disable'||!String(data.get('serverName')||'').trim()?{}:{serverName:String(data.get('serverName')).trim()})},username:String(data.get('username')||'').trim(),...(mode==='external'?{secretRef:kind==='systemd-credential'?{kind,name:String(data.get('externalName')||'').trim()}:{kind,path:String(data.get('externalPath')||'').trim()}}:{}),contractRange:'^1.0.0'},expectedGeneration:Number(status.generation||0),...(mode==='direct'?{secretMaterial:String(data.get('password')||'')}:{})}};
const mutate=async(url,method)=>json(url,{method,headers:{'X-VastPlan-CSRF':await token()},body:JSON.stringify(payload())});
const refresh=async()=>{try{status=await json('/v1/bootstrap/platform-control');if(status.phase==='ready'){message('平台控制数据库已就绪，正在进入平台管理中心…');setTimeout(()=>location.replace('/operations'),400);return}if(status.phase==='testing')message('正在测试数据库连接…');else if(status.phase==='initializing')message('正在初始化平台 Schema…');else if(status.phase==='recovery')message('当前配置不可用，可以修正后重新初始化。'+(status.code?' '+friendly(status.code):''),true);else message('平台控制数据库尚未配置。')}catch(error){message(error.message,true)}};
form.tlsMode.addEventListener('change',()=>document.querySelector('#serverNameRow').classList.toggle('hidden',form.tlsMode.value==='disable'));
const secretVisibility=()=>{const external=form.secretMode.value==='external',systemd=form.externalKind.value==='systemd-credential';document.querySelector('.secret-direct').classList.toggle('hidden',external);document.querySelector('.secret-external').classList.toggle('hidden',!external);document.querySelector('.secret-file').classList.toggle('hidden',!external||systemd);document.querySelector('.secret-systemd').classList.toggle('hidden',!external||!systemd);form.password.required=!external;form.externalName.required=external&&systemd;form.externalPath.required=external&&!systemd};
form.secretMode.addEventListener('change',secretVisibility);form.externalKind.addEventListener('change',secretVisibility);secretVisibility();
test.addEventListener('click',async()=>{if(!form.reportValidity())return;busy(true);message('正在测试数据库连接…');try{await mutate('/v1/bootstrap/platform-control/test','POST');message('连接测试成功，尚未写入配置。')}catch(error){csrf='';message(error.message,true)}finally{busy(false)}});
form.addEventListener('submit',async event=>{event.preventDefault();if(!form.reportValidity())return;busy(true);message('正在初始化 Schema 并提交配置，请勿关闭页面…');try{status=await mutate('/v1/bootstrap/platform-control','PUT');message('配置已提交，正在启动完整平台服务…');setTimeout(refresh,600)}catch(error){csrf='';message(error.message,true);busy(false)}});
refresh();
</script></body></html>`;
}
