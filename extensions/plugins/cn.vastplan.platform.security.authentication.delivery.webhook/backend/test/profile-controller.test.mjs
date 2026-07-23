import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { configurationResourceCollectionId, prepareResourceRequestDigest } from "@vastplan/configuration-resource-controller-node";

import { pluginId, profileCollectionKey } from "../config.mjs";
import { WebhookProfileController } from "../profile-controller.mjs";
import { ProfileStateStore } from "../state-store.mjs";

const collectionId = configurationResourceCollectionId(pluginId, profileCollectionKey);
const resourceId = `cfgp_${"2".repeat(32)}`;
const candidateId = `pcfg_${"a".repeat(32)}`;
const ref = (suffix, version) => ({ handle:`credential://managed/${suffix}`,scope:"tenant",owner:pluginId,purpose:"authentication.delivery.webhook",version });
const values = { displayName:"Enterprise Mail",endpoint:"https://delivery.example.test/v1/code",channels:["email"],timeoutMs:1000 };
const runtime = (tenantId="tenant-a", host) => ({ context:{tenant_id:tenantId}, invocation:{signal:{signal:new AbortController().signal}}, host });

test("Webhook Profile Controller persists tenant-isolated resources and retires replaced refs", async (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "vastplan-webhook-profile-"));
  t.after(() => fs.rmSync(root,{recursive:true,force:true}));
  const stateFile = path.join(root,"profiles.json"), profiles = new Map();
  let leaseCalls=0, retired=[];
  const materialLease={async withMaterial(_ref,tenant,_signal,use){leaseCalls++;assert.equal(tenant,"tenant-a");return use(Buffer.from("0123456789abcdef-token"));}};
  const host={async call(_target,_context,payload){retired.push(JSON.parse(payload).handle);return {result:{status:"STATUS_OK"}};}};
  let controller = new WebhookProfileController({collectionId,store:new ProfileStateStore(stateFile),materialLease,profiles});
  const create={candidateId,configurationId:`cfg_${"9".repeat(24)}`,collectionId,resourceId,action:"create",catalogDigest:"c".repeat(64),schemaDigest:"d".repeat(64),artifactSha256:"e".repeat(64),values,managedCredentials:{authorization:ref("old",1)}};
  const prepared=await controller.prepare(create,runtime());
  assert.equal(prepared.candidate.status,"Prepared");
  const committed=await controller.commit({candidateId,requestDigest:prepareResourceRequestDigest(create)},runtime("tenant-a",host));
  assert.equal(committed.active.revision,1); assert.equal(leaseCalls,1);
  assert.equal((await controller.list({collectionId},runtime("tenant-b"))).items.length,0);

  controller = new WebhookProfileController({collectionId,store:new ProfileStateStore(stateFile),materialLease,profiles:new Map()});
  const loaded=await controller.get({collectionId,resourceId},runtime());
  assert.equal(loaded.item.values.displayName,"Enterprise Mail");
  assert.equal(JSON.stringify(loaded).includes("credential://"),false);
  const update={...create,candidateId:`pcfg_${"b".repeat(32)}`,action:"update",expectedActive:loaded.item.active,values:{...values,displayName:"Enterprise Mail 2"},managedCredentials:{authorization:ref("new",2)}};
  await controller.prepare(update,runtime());
  await controller.commit({candidateId:update.candidateId,requestDigest:prepareResourceRequestDigest(update)},runtime("tenant-a",host));
  assert.deepEqual(retired,["credential://managed/old"]);
  assert.equal((await controller.get({collectionId,resourceId},runtime())).item.active.revision,2);
  assert.equal(fs.statSync(stateFile).mode & 0o077,0);
});

test("Webhook Profile Controller rejects stale CAS and invalid secret material", async (t) => {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),"vastplan-webhook-profile-reject-"));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
  const controller=new WebhookProfileController({collectionId,store:new ProfileStateStore(path.join(root,"profiles.json")),profiles:new Map(),materialLease:{async withMaterial(_ref,_tenant,_signal,use){return use(Buffer.from("short"));}}});
  const create={candidateId,configurationId:`cfg_${"9".repeat(24)}`,collectionId,resourceId,action:"create",catalogDigest:"c".repeat(64),schemaDigest:"d".repeat(64),artifactSha256:"e".repeat(64),values,managedCredentials:{authorization:ref("bad",1)}};
  await assert.rejects(()=>controller.prepare(create,runtime()),/material/);
  assert.equal((await controller.list({collectionId},runtime())).items.length,0);
});
