import { RequestJSONError, requireJSONObject } from "./request-json";

export function installationApprovalEvidence(value: unknown): Readonly<Record<string, unknown>> {
  const request = requireJSONObject(value);
  if (Object.keys(request).some((key) => key !== "evidence")) throw new RequestJSONError("审批请求字段无效");
  if (request.evidence === undefined) return Object.freeze({});
  const evidence = requireJSONObject(request.evidence);
  if (Object.keys(evidence).length > 32 || Object.keys(evidence).some((key) => !/^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$/.test(key))) throw new RequestJSONError("审批证据字段无效");
  return evidence;
}
