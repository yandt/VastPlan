import { describe,expect,it,vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { createAPIExposurePage, createDataPlaneExposurePage } from "./index";

describe("API Exposure Workbench",()=>{
  it("uses governed collection actions",async()=>{
    const client={listAPIExposures:vi.fn(async()=>[{id:7,status:"Draft",exposure:{id:"exp_aaaaaaaaaaaaaaaaaaaa",revision:1,routeKey:"aaaaaaaaaaaaaaaaaaaa",displayName:"Demo",hosts:["api.example.com"],contract:{contractId:"platform.demo.api",contractVersion:"1.0.0"}},updatedAt:"2026-07-22T00:00:00Z"}]),submitAPIExposure:vi.fn(async()=>({}))} as unknown as PlatformAdminClient;
    const page=createAPIExposurePage(client,"core");const result=await page.load({mode:"page",page:1,pageSize:20,filters:{}},new AbortController().signal);expect(result.total).toBe(1);expect(result.items[0]?.routeKey).toBe("aaaaaaaaaaaaaaaaaaaa");await page.runAction?.({action:page.collection.actions![1]!,selected:result.items,refresh:()=>undefined},new AbortController().signal);expect(client.submitAPIExposure).toHaveBeenCalledWith(7);
  });
  it("uses the same governed lifecycle for data-plane revisions", async () => {
    const client = { listDataPlaneExposures: vi.fn(async () => [{ id: 9, status: "Approved", exposure: { id: "dpx_bbbbbbbbbbbbbbbbbbbb", routeKey: "bbbbbbbbbbbbbbbbbbbb", service: { pluginId: "cn.vastplan.repo", contributionId: "artifact-data" }, allowedModes: ["ticket-redirect"], hosts: ["repo.example.com"] }, updatedAt: "2026-07-22T00:00:00Z" }]), publishDataPlaneExposure: vi.fn(async () => ({})) } as unknown as PlatformAdminClient;
    const page = createDataPlaneExposurePage(client, "core");
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(result.total).toBe(1);
    await page.runAction?.({ action: page.collection.actions![3]!, selected: result.items, refresh: () => undefined }, new AbortController().signal);
    expect(client.publishDataPlaneExposure).toHaveBeenCalledWith(9);
  });
});
