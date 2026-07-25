import type { AuthorizationPermission, PlatformAdminClient } from "@vastplan/platform-admin";
import { defineCollectionPage, message, type CollectionPageDefinition } from "@vastplan/workbench-sdk";
import { namespace, page } from "../model.js";

type PermissionRow = AuthorizationPermission & Record<string, unknown>;

export function permissionsPage(client: PlatformAdminClient): CollectionPageDefinition<PermissionRow> {
  return defineCollectionPage<PermissionRow>({
    id:"platform.authorization.permissions", path:"/settings/authorization/permissions", title:message(namespace,"permissions.title","权限目录"),
    description:message(namespace,"permissions.description","权限由已验证插件清单声明，角色管理不能创建权限代码。"), requiredPermissions:["platform.authorization.catalog"], navigation:{id:"platform.authorization.permissions",label:message(namespace,"permissions.navigation","权限目录"),zone:"settings",groupID:"platform.authorization",order:10},
    collection:{id:"authorization-permissions",title:message(namespace,"permissions.title","权限目录"),view:"table",query:{mode:"page",defaultPageSize:20,pageSizeOptions:[20,50,100]},filters:[{id:"search",label:message(namespace,"filter.permission","权限代码或插件"),kind:"text"}],columns:[
      {key:"code",label:message(namespace,"column.code","权限代码"),defaultVisible:true,minWidth:280},{key:"title",label:message(namespace,"column.title","名称"),defaultVisible:true,minWidth:160},{key:"scope",label:message(namespace,"column.scope","作用域"),defaultVisible:true},{key:"risk",label:message(namespace,"column.risk","风险"),format:"status",valueLabels:{low:"Low",medium:"Medium",high:"High",critical:"Critical"},statusTones:{low:"neutral",medium:"info",high:"warning",critical:"error"},defaultVisible:true},{key:"pluginId",label:message(namespace,"column.plugin","所属插件"),defaultVisible:true,minWidth:280},{key:"assignable",label:message(namespace,"column.assignable","可分配"),format:"status",valueLabels:{true:message(namespace,"yes","是"),false:message(namespace,"no","否")},statusTones:{true:"success",false:"neutral"},defaultVisible:true}
    ],preferences:{allowedColumns:["code","title","scope","risk","pluginId","assignable"],density:true}},
    async load(query,signal){const state=await client.getAuthorizationPolicy();if(signal.aborted)return{items:[],total:0};return page(state.catalog.permissions as PermissionRow[],query,(row,text)=>row.code.toLowerCase().includes(text)||row.pluginId.toLowerCase().includes(text));}
  });
}
