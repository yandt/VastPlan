import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireEmptyJSONObject, requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.api-exposure";
const permissionPrefix = "platform.api-exposure";

export class PlatformAPIExposureRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] === "api-exposures") return this.handleHTTP(parts, principal, target, request, response, signal);
    if (parts[0] === "data-plane-exposures") return this.handleDataPlane(parts, principal, target, request, response, signal);
    return false;
  }

  private async handleHTTP(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    const method = request.method ?? "GET";
    if (parts.length === 1 && (method === "GET" || method === "HEAD")) return this.call(principal,target,"list",false,`${permissionPrefix}.read`,{},response,signal,method === "HEAD");
    if (parts.length === 1 && method === "POST") { await withRequestJSON(request,response,async body => { await this.call(principal,target,"createDraft",true,`${permissionPrefix}.edit`,requireJSONObject(body),response,signal); }); return true; }
    if (parts[1] === "exposure" && parts.length === 4 && parts[3] === "retire" && method === "POST") {
      const exposureId = resourceName(parts[2],160); if (exposureId === undefined) return reject(response,400,"invalid_resource_name",method);
      await withRequestJSON(request,response,async body => { requireEmptyJSONObject(body); await this.call(principal,target,"retire",true,`${permissionPrefix}.publish`,{exposureId},response,signal); }); return true;
    }
    const revisionId = positiveRevision(parts[1]);
    if (revisionId === undefined) return reject(response,400,"invalid_revision",method);
    if (parts.length === 2 && method === "PUT") { await withRequestJSON(request,response,async body => { await this.call(principal,target,"updateDraft",true,`${permissionPrefix}.edit`,{...requireJSONObject(body),revisionId},response,signal); }); return true; }
    if (parts.length === 3 && method === "POST") return this.transition(parts[2],revisionId,principal,target,request,response,signal,false);
    return reject(response,405,"method_not_allowed",method);
  }

  private async handleDataPlane(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    const method = request.method ?? "GET";
    if (parts.length === 1 && (method === "GET" || method === "HEAD")) return this.call(principal,target,"listDataPlanes",false,`${permissionPrefix}.read`,{},response,signal,method === "HEAD");
    if (parts.length === 1 && method === "POST") { await withRequestJSON(request,response,async body => { await this.call(principal,target,"createDataPlaneDraft",true,`${permissionPrefix}.edit`,requireJSONObject(body),response,signal); }); return true; }
    if (parts[1] === "exposure" && parts.length === 4 && parts[3] === "retire" && method === "POST") {
      const exposureId = resourceName(parts[2],160); if (exposureId === undefined) return reject(response,400,"invalid_resource_name",method);
      await withRequestJSON(request,response,async body => { requireEmptyJSONObject(body); await this.call(principal,target,"retireDataPlane",true,`${permissionPrefix}.publish`,{exposureId},response,signal); }); return true;
    }
    const revisionId = positiveRevision(parts[1]);
    if (revisionId === undefined || parts.length !== 3 || method !== "POST") return reject(response,405,"method_not_allowed",method);
    return this.transition(parts[2],revisionId,principal,target,request,response,signal,true);
  }

  private async transition(action: string | undefined, revisionId: number, principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal, dataPlane: boolean): Promise<true> {
    const actions: Record<string,{permission:string;operation:string}> = {
      submit:{permission:`${permissionPrefix}.edit`,operation:dataPlane?"submitDataPlane":"submit"},
      approve:{permission:`${permissionPrefix}.approve`,operation:dataPlane?"approveDataPlane":"approve"},
      publish:{permission:`${permissionPrefix}.publish`,operation:dataPlane?"publishDataPlane":"publish"},
    };
    const selected = actions[action ?? ""]; if (selected === undefined) return reject(response,404,"not_found",request.method ?? "POST");
    await withRequestJSON(request,response,async body => { requireEmptyJSONObject(body); await this.call(principal,target,selected.operation,true,selected.permission,{revisionId},response,signal); });
    return true;
  }

  private async call(principal:Principal,target:PlatformManagementTarget,operation:string,write:boolean,permission:string,payload:unknown,response:ServerResponse,signal:AbortSignal,head=false):Promise<true> {
    if (!authorizePlatformOperation(this.client,target,capability,operation,write,response) || !requirePlatformRole(principal,permission,response)) return true;
    await sendPlatformResponse({client:this.client,principal,target,capability,operation,write,payload,response,signal,head}); return true;
  }
}

function positiveRevision(value:string|undefined):number|undefined { if (value===undefined || !/^[1-9][0-9]*$/.test(value)) return undefined; const parsed=Number(value); return Number.isSafeInteger(parsed)?parsed:undefined; }
function reject(response:ServerResponse,status:number,code:string,method:string):true { sendAPIError(response,status,code,method==="HEAD"); return true; }
