import { randomBytes } from "node:crypto";
import { createInbox, headers, type Msg, type NatsConnection, type Subscription } from "@nats-io/transport-node";
import { AddressingProtocolCodec } from "./protocol-codec.js";
import { instanceSubject } from "./capability-directory.js";
import { NodeTransportSecurity, transportHeaders, type SignedHeaders } from "./transport-security.js";
import type { AddressingResponse, CallContext, CallTarget, CapabilityAnnouncement, CapabilityDirectoryPort } from "./types.js";

const cancelSubject = "vp.rpc.cancel.v1";
const defaultLimits = Object.freeze({ maxPayloadBytes: 4 << 20, maxMetadataBytes: 16 << 10, maxConcurrentCalls: 256, timeoutMs: 30_000 });

export interface AddressingClientOptions {
  connection: NatsConnection;
  directory: CapabilityDirectoryPort;
  security: NodeTransportSecurity;
  codec: AddressingProtocolCodec;
  limits?: Partial<typeof defaultLimits>;
}

export class AddressingTransportError extends Error {
  public constructor(public readonly code: string, message: string, public readonly retryable = false) {
    super(message);
    this.name = "AddressingTransportError";
  }
}

/** Unary Node implementation of the existing NATS/Protobuf Addressing v1 wire. */
export class NodeAddressingClient {
  private readonly limits: typeof defaultLimits;
  private activeCalls = 0;

  public constructor(private readonly options: AddressingClientOptions) {
    this.limits = Object.freeze({ ...defaultLimits, ...options.limits });
  }

  public async invoke(target: CallTarget, context: CallContext, payload: Uint8Array, signal?: AbortSignal): Promise<AddressingResponse> {
    this.validateInput(target, context, payload);
    if (this.activeCalls >= this.limits.maxConcurrentCalls) throw new AddressingTransportError("concurrency_limited", "Addressing 调用并发达到上限", true);
    this.activeCalls += 1;
    try {
      const { instance, allowedResponseKeys } = this.resolve(target);
      const subject = target.instance_id === undefined ? instance.subject : instanceSubject(instance);
      const requestID = randomBytes(12).toString("hex");
      const boundedContext = withDeadline(context, this.limits.timeoutMs);
      const request = this.options.codec.encodeRequest(requestID, target, boundedContext, payload);
      const response = await this.request(subject, requestID, request, allowedResponseKeys, signal);
      const decoded = this.options.codec.decodeResponse(response.data);
      if (decoded.request_id !== requestID) throw new AddressingTransportError("remote_invalid_response", "远端响应 request_id 不匹配");
      if (decoded.transport_error !== undefined) {
        throw new AddressingTransportError(decoded.transport_error.code ?? "remote_invoke_failed", decoded.transport_error.message ?? "远端 Addressing 调用失败", decoded.transport_error.retryable === true);
      }
      if (decoded.result === undefined) throw new AddressingTransportError("remote_invalid_response", "远端响应缺少 CallResult");
      const responsePayload = decoded.payload ?? new Uint8Array();
      if (responsePayload.byteLength > this.limits.maxPayloadBytes) throw new AddressingTransportError("payload_too_large", "Addressing 响应 payload 超过上限");
      return { result: decoded.result, payload: responsePayload };
    } finally {
      this.activeCalls -= 1;
    }
  }

  private resolve(target: CallTarget): { instance: CapabilityAnnouncement; allowedResponseKeys: ReadonlySet<string> } {
    const instances = this.options.directory.instances({
      capability: target.capability,
      ...(target.logical_service === undefined ? {} : { logicalService: target.logical_service }),
      ...(target.routing_domain === undefined ? {} : { routingDomain: target.routing_domain }),
      ...(target.partition_key === undefined ? {} : { partitionKey: target.partition_key }),
      ...(target.instance_id === undefined ? {} : { instanceId: target.instance_id }),
    });
    if (instances.length === 0) throw new AddressingTransportError("capability_not_found", `全局能力目录中没有健康实例: ${target.capability}`, true);
    if (target.instance_id === undefined && target.logical_service === undefined && target.routing_domain === undefined) {
      const subjects = new Set(instances.map((instance) => instance.subject));
      if (subjects.size > 1) throw new AddressingTransportError("routing_ambiguous", `capability ${target.capability} 存在多个路由域`);
    }
    const instance = instances[0]!;
    const eligible = target.instance_id === undefined ? instances.filter((candidate) => candidate.subject === instance.subject) : [instance];
    const allowedResponseKeys = new Set(eligible.map((candidate) => candidate.transport_public_key).filter((value): value is string => value !== undefined && value !== ""));
    if (allowedResponseKeys.size === 0) throw new AddressingTransportError("directory_identity_missing", `capability ${target.capability} 没有已绑定的传输身份`);
    return { instance, allowedResponseKeys };
  }

  private request(subject: string, requestID: string, payload: Uint8Array, allowedResponseKeys: ReadonlySet<string>, signal?: AbortSignal): Promise<Msg> {
    if (signal?.aborted === true) return Promise.reject(signal.reason ?? new Error("Addressing 调用已取消"));
    const reply = createInbox();
    const signed = this.options.security.sign(subject, payload);
    const requestHeaders = headers();
    writeHeaders(requestHeaders, signed);
    return new Promise<Msg>((resolve, reject) => {
      let settled = false;
      let subscription: Subscription | undefined;
      const finish = (error?: unknown, message?: Msg) => {
        if (settled) return;
        settled = true;
        clearTimeout(timeout);
        signal?.removeEventListener("abort", abort);
        subscription?.unsubscribe();
        if (error !== undefined) reject(error);
        else resolve(message!);
      };
      const abort = () => {
        this.options.connection.publish(cancelSubject, new TextEncoder().encode(requestID));
        finish(signal?.reason ?? new Error("Addressing 调用已取消"));
      };
      const timeout = setTimeout(() => {
        this.options.connection.publish(cancelSubject, new TextEncoder().encode(requestID));
        finish(new AddressingTransportError("deadline_exceeded", `Addressing 调用超时: ${subject}`, true));
      }, this.limits.timeoutMs);
      subscription = this.options.connection.subscribe(reply, { max: 1, callback: (error, message) => {
        if (error !== null) return finish(error);
        try {
          const identity = this.options.security.verify(message.subject, message.data, readHeaders(message), true);
          if (!allowedResponseKeys.has(identity.publicKey)) throw new Error("Addressing 响应身份未绑定到所选 capability 实例");
          finish(undefined, message);
        } catch (verifyError) {
          finish(verifyError);
        }
      } });
      signal?.addEventListener("abort", abort, { once: true });
      try { this.options.connection.publish(subject, payload, { reply, headers: requestHeaders }); }
      catch (error) { finish(error); }
    });
  }

  private validateInput(target: CallTarget, context: CallContext, payload: Uint8Array): void {
    if (!target.capability || !target.extension_point) throw new AddressingTransportError("wire_invalid_request", "调用目标 capability 与 extension_point 不能为空");
    if (payload.byteLength > this.limits.maxPayloadBytes) throw new AddressingTransportError("payload_too_large", "Addressing 请求 payload 超过上限");
    if (this.options.codec.contextSize(context) > this.limits.maxMetadataBytes) throw new AddressingTransportError("metadata_too_large", "Addressing CallContext 超过上限");
  }
}

function withDeadline(context: CallContext, timeoutMs: number): CallContext {
  const localDeadline = Date.now() + timeoutMs;
  const declared = context.deadline_unix_ms;
  const deadline = declared !== undefined && Number.isFinite(declared) ? Math.min(localDeadline, declared) : localDeadline;
  return { ...context, deadline_unix_ms: Math.trunc(deadline) };
}

function writeHeaders(target: ReturnType<typeof headers>, values: SignedHeaders): void {
  target.set(transportHeaders.publicKey, values.publicKey);
  target.set(transportHeaders.timestamp, values.timestamp);
  target.set(transportHeaders.nonce, values.nonce);
  target.set(transportHeaders.signature, values.signature);
}

function readHeaders(message: Msg): SignedHeaders {
  const values = message.headers;
  if (values === undefined) throw new Error("Addressing 响应缺少传输签名");
  return {
    publicKey: values.get(transportHeaders.publicKey),
    timestamp: values.get(transportHeaders.timestamp),
    nonce: values.get(transportHeaders.nonce),
    signature: values.get(transportHeaders.signature),
  };
}
