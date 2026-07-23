import assert from "node:assert/strict";
import test from "node:test";
import { normalizeProfile } from "../config.mjs";
import { WebhookDelivery } from "../delivery.mjs";

const ref = {handle:"credential://managed/webhook-token",scope:"tenant",owner:"cn.vastplan.platform.security.authentication.delivery.webhook",purpose:"authentication.delivery.webhook",version:1};
const resourceId = `cfgp_${"2".repeat(32)}`;
const values = {displayName:"Enterprise Mail",endpoint:"https://delivery.example.test/v1/code",channels:["email"],timeoutMs:1000};
const configuration = () => new Map([[`tenant-a\0${resourceId}`, normalizeProfile(resourceId, values, {authorization:ref})]]);
const request = {challengeId:"challenge.12345678",deliveryProfileId:resourceId,channel:"email",identifier:"alice@example.com",locale:"zh-CN",code:"123456",expiresAt:new Date(Date.now()+300000).toISOString()};
const context = {tenant_id:"tenant-a",call_path:["authentication.provider/enterprise-one-time-code#continue","tool.package/foundation.security.authentication.delivery#deliver"]};

test("Webhook Delivery obtains authorization only through Material Lease", async () => {
  let leaseCalls = 0, observed;
  const service = new WebhookDelivery(configuration(), {
    materialLease:{async withMaterial(actualRef, tenant, _signal, use){ leaseCalls++; assert.deepEqual(actualRef,ref); assert.equal(tenant,"tenant-a"); const material=Buffer.from("0123456789abcdef-token"); try{return await use(material)} finally{material.fill(0)} }},
    fetcher:async (url, init) => { observed={url,authorization:init.headers.authorization,body:JSON.parse(init.body.toString())}; return new Response(JSON.stringify({accepted:true,subjectId:"enterprise.alice"}),{status:200,headers:{"content-type":"application/json"}}); },
  });
  assert.deepEqual(await service.deliver(request,context,new AbortController().signal),{accepted:true,subjectId:"enterprise.alice"});
  assert.equal(leaseCalls,1); assert.equal(observed.url,"https://delivery.example.test/v1/code"); assert.equal(observed.authorization,"Bearer 0123456789abcdef-token"); assert.equal(observed.body.code,"123456");
});

test("Webhook Delivery rejects untrusted callers and malformed upstream results", async () => {
  const materialLease={async withMaterial(_ref,_tenant,_signal,use){return use(Buffer.from("0123456789abcdef"))}};
  const service = new WebhookDelivery(configuration(),{materialLease,fetcher:async()=>new Response(JSON.stringify({accepted:false,subjectId:"leak"}),{status:200})});
  await assert.rejects(()=>service.deliver(request,{tenant_id:"tenant-a",call_path:[]},new AbortController().signal),/OTP Provider/);
  await assert.rejects(()=>service.deliver(request,context,new AbortController().signal),/response|Delivery/i);
  await assert.rejects(()=>service.deliver({...request,token:"secret"},context,new AbortController().signal),/field|字段/i);
});

test("Webhook Delivery configuration requires HTTPS and managed credentials", () => {
  assert.throws(()=>normalizeProfile(resourceId,{...values,endpoint:"http://delivery.example.test"},{authorization:ref}),/HTTPS/);
  assert.throws(()=>normalizeProfile(resourceId,values,{authorization:{...ref,handle:"plain-secret"}}),/authorizationRef/);
});
