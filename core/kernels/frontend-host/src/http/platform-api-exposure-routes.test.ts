import { afterEach, describe, expect, it } from "vitest";
import { managementBinding, recordingPlatformInvoker, startPlatformManagementTestServer, type PlatformInvocation } from "../testing/platform-management-test-harness";

const close:(()=>Promise<void>)[]=[];
afterEach(async()=>Promise.all(close.splice(0).map(action=>action())));

describe("Platform API Exposure BFF",()=>{
  it("uses dictionary-routed lifecycle operations and injects path identities",async()=>{
    const calls:PlatformInvocation[]=[];
    const server=await startPlatformManagementTestServer(recordingPlatformInvoker(calls),["platform.api-exposure.read","platform.api-exposure.edit","platform.api-exposure.approve","platform.api-exposure.publish"],binding());close.push(server.close);
    const base=`${server.origin}/v1/portals/operations/platform/services/core`;
    expect((await fetch(`${base}/api-exposures`,{headers:server.readHeaders})).status).toBe(200);
    expect((await fetch(`${base}/api-exposures`,{method:"POST",headers:server.writeHeaders,body:'{"input":{}}'})).status).toBe(200);
    expect((await fetch(`${base}/api-exposures/7`,{method:"PUT",headers:server.writeHeaders,body:'{"expectedRevision":2}'})).status).toBe(200);
    for(const action of ["submit","approve","publish"]) expect((await fetch(`${base}/api-exposures/7/${action}`,{method:"POST",headers:server.writeHeaders,body:"{}"})).status).toBe(200);
    expect((await fetch(`${base}/data-plane-exposures/9/publish`,{method:"POST",headers:server.writeHeaders,body:"{}"})).status).toBe(200);
    expect((await fetch(`${base}/data-plane-exposures/exposure/dpx_aaaaaaaaaaaaaaaaaaaa/retire`,{method:"POST",headers:server.writeHeaders,body:"{}"})).status).toBe(200);
    expect(calls.map(call=>call.operation)).toEqual(["list","createDraft","updateDraft","submit","approve","publish","publishDataPlane","retireDataPlane"]);
    expect(calls[2].payload).toEqual({expectedRevision:2,revisionId:7});
  });
});

function binding(){return managementBinding([{capability:"platform.api-exposure",read:["list","listDataPlanes"],write:["createDraft","updateDraft","submit","approve","publish","retire","createDataPlaneDraft","submitDataPlane","approveDataPlane","publishDataPlane","retireDataPlane"]}]);}
