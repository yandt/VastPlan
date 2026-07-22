import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.authorization";
const permission = "platform.authorization";

export class PlatformAuthorizationRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "authorization") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1 && (method === "GET" || method === "HEAD")) return this.call(principal, target, "get", false, `${permission}.catalog`, {}, response, signal, method === "HEAD");
    if (parts.length === 2 && parts[1] === "audit" && (method === "GET" || method === "HEAD")) return this.call(principal, target, "listAudit", false, `${permission}.audit`, {}, response, signal, method === "HEAD");
    if (parts.length === 2 && parts[1] === "roles" && method === "POST") return this.bodyCall(principal, target, "createRole", `${permission}.role`, request, response, signal);
    if (parts.length === 2 && parts[1] === "bindings" && method === "POST") return this.bodyCall(principal, target, "createBinding", `${permission}.binding`, request, response, signal);
    if (parts.length === 2 && parts[1] === "revocations" && method === "POST") return this.bodyCall(principal, target, "revoke", `${permission}.revoke`, request, response, signal);
    if (parts.length === 2 && parts[1] === "snapshots" && method === "POST") return this.bodyCall(principal, target, "publishSnapshot", `${permission}.publish`, request, response, signal);
    if (parts.length < 4 || (parts[1] !== "roles" && parts[1] !== "bindings")) return reject(response, 404, "not_found", method);
    const id = resourceName(parts[2], 160), revision = positiveRevision(parts[3]);
    if (id === undefined || revision === undefined) return reject(response, 400, "invalid_revision", method);
    if (parts[1] === "roles" && parts.length === 4 && method === "PUT") {
      await withRequestJSON(request, response, async body => { await this.call(principal, target, "updateRole", true, `${permission}.role`, { ...requireJSONObject(body), id, revision }, response, signal); });
      return true;
    }
		if (parts[1] === "bindings" && parts.length === 4 && method === "PUT") {
			await withRequestJSON(request, response, async body => { await this.call(principal, target, "updateBinding", true, `${permission}.binding`, { ...requireJSONObject(body), id, revision }, response, signal); });
			return true;
		}
    if (parts.length !== 5 || method !== "POST") return reject(response, 405, "method_not_allowed", method);
    const operation = transitionOperation(parts[1], parts[4]);
    if (operation === undefined) return reject(response, 404, "not_found", method);
    const required = parts[4] === "approve" ? `${permission}.approve` : parts[4] === "publish" ? `${permission}.publish` : parts[4] === "retire" ? `${permission}.${parts[1] === "roles" ? "role" : "binding"}` : `${permission}.${parts[1] === "roles" ? "role" : "binding"}`;
    await withRequestJSON(request, response, async body => { const value = requireJSONObject(body); await this.call(principal, target, operation, true, required, { ...value, id, revision }, response, signal); });
    return true;
  }

  private async bodyCall(principal:Principal,target:PlatformManagementTarget,operation:string,role:string,request:IncomingMessage,response:ServerResponse,signal:AbortSignal):Promise<true>{
    await withRequestJSON(request,response,async body=>{ await this.call(principal,target,operation,true,role,requireJSONObject(body),response,signal); }); return true;
  }
  private async call(principal:Principal,target:PlatformManagementTarget,operation:string,write:boolean,role:string,payload:unknown,response:ServerResponse,signal:AbortSignal,head=false):Promise<true>{
    if(!authorizePlatformOperation(this.client,target,capability,operation,write,response)||!requirePlatformRole(principal,role,response))return true;
    await sendPlatformResponse({client:this.client,principal,target,capability,operation,write,payload,response,signal,head});return true;
  }
}

function transitionOperation(kind:string|undefined,action:string|undefined):string|undefined{ const prefix=kind==="roles"?"Role":kind==="bindings"?"Binding":""; if(prefix===""||action===undefined||!["submit","approve","publish","retire"].includes(action))return undefined; return action+prefix; }
function positiveRevision(value:string|undefined):number|undefined{if(value===undefined||!/^[1-9][0-9]*$/.test(value))return undefined;const parsed=Number(value);return Number.isSafeInteger(parsed)?parsed:undefined;}
function reject(response:ServerResponse,status:number,code:string,method:string):true{sendAPIError(response,status,code,method==="HEAD");return true;}
