import { EventEmitter } from 'node:events';
import { randomUUID } from 'node:crypto';
import { AsyncLocalStorage } from 'node:async_hooks';

import { loadProtocol } from './protocol.mjs';

const MAGIC = 'VASTPLAN_PLUGIN_V1';
const FEATURES = ['channel.cancel.v1', 'event.publish.v1'];
const MAX_PAYLOAD_BYTES = 4 << 20;
const SESSION_METADATA = 'vastplan-session-id';

export class Contribution {
  constructor({ extensionPoint, id, descriptor, priority = 0, handlers = {} }) {
    if (!extensionPoint || !id) throw new Error('Contribution extensionPoint/id 不能为空');
    this.extensionPoint = extensionPoint;
    this.id = id;
    this.priority = priority;
    this.descriptor = Buffer.isBuffer(descriptor) ? descriptor : Buffer.from(JSON.stringify(descriptor ?? {}));
    this.handlers = new Map(Object.entries(handlers));
  }

  wire() {
    return {
      extension_point: this.extensionPoint,
      id: this.id,
      priority: this.priority,
      descriptor_json: this.descriptor,
    };
  }
}

export class InvocationContext {
  constructor(request) {
    this.requestId = request.request_id;
    this.delegationToken = request.delegation_token ?? '';
    this.signal = new AbortController();
    const rawDeadline = request.context?.deadline_unix_ms;
    this.deadlineUnixMs = rawDeadline === undefined || rawDeadline === null || rawDeadline === '0'
      ? undefined : Number(rawDeadline);
  }

  get cancelled() {
    return this.signal.signal.aborted || (this.deadlineUnixMs !== undefined && Date.now() >= this.deadlineUnixMs);
  }

  throwIfCancelled() {
    if (this.cancelled) throw new Error('VastPlan invocation cancelled or timed out');
  }
}

export class Plugin extends EventEmitter {
  constructor({ id, version, engines }) {
    super();
    this.id = id;
    this.version = version;
    this.engines = { ...engines };
    this.contributions = [];
    this.routes = new Map();
    this.pending = new Map();
    this.calls = new Map();
    this.features = new Set();
    this.invocationLocal = new AsyncLocalStorage();
    this.active = false;
    this.stream = undefined;
  }

  contribute(contribution) {
    if (this.stream) throw new Error('serve 后请使用 registerContribution');
    this.#install(contribution);
  }

  async serve() {
    if (process.env.VASTPLAN_PLUGIN_MAGIC !== MAGIC) {
      throw new Error('magic cookie 不匹配：插件必须由 VastPlan 宿主拉起');
    }
    const address = process.env.VASTPLAN_HOST_ADDR;
    if (!address) throw new Error('未注入宿主地址 VASTPLAN_HOST_ADDR');
    const { grpc, PluginHost } = loadProtocol();
    this.client = new PluginHost(address, grpc.credentials.createInsecure(), {
      'grpc.max_receive_message_length': 4_456_448,
      'grpc.max_send_message_length': 4_456_448,
    });
    const ack = await new Promise((resolveAck, reject) => {
      this.client.Handshake({
        proto_versions: [1],
        magic: MAGIC,
        plugin_id: this.id,
        plugin_version: this.version,
        engines: this.engines,
        launch_token: process.env.VASTPLAN_LAUNCH_TOKEN ?? '',
        features: FEATURES,
      }, (error, response) => error ? reject(error) : resolveAck(response));
    });
    if (ack.negotiated_proto !== 1) {
      throw new Error(`宿主协商了不支持的协议版本 ${ack.negotiated_proto}`);
    }
    this.features = new Set(ack.negotiated_features ?? []);
    const metadata = new grpc.Metadata();
    metadata.set(SESSION_METADATA, ack.session_id);
    this.stream = this.client.Channel(metadata);
    this.stream.on('data', (message) => void this.#dispatch(message));
    this.stream.on('error', (error) => this.emit('error', error));
    this.stream.write({
      declare: { contributions: this.contributions.map((item) => item.wire()) },
    });
    return new Promise((resolveServe, reject) => {
      this.stream.once('end', resolveServe);
      this.stream.once('close', resolveServe);
      this.stream.once('error', reject);
    });
  }

  async call(target, context, payload, timeoutMs = 30_000) {
    const bytes = Buffer.isBuffer(payload) ? payload : Buffer.from(payload ?? '');
    if (bytes.length > MAX_PAYLOAD_BYTES) throw new Error('HostCall payload 超过协议上限');
    const requestId = `hc-${randomUUID()}`;
    const hostCall = { request_id: requestId, target, context, payload: bytes };
    const delegationToken = this.invocationLocal.getStore()?.delegationToken;
    if (delegationToken) hostCall.delegation_token = delegationToken;
    const response = await this.#request(requestId, {
      host_call: hostCall,
    }, timeoutMs);
    return response.host_call_result;
  }

  publishEvent(event) {
    this.#requireFeature('event.publish.v1');
    this.#send({ event: { event } });
  }

  async shutdown() {
    this.active = false;
    this.stream?.end();
    this.client?.close();
  }

  #install(contribution) {
    this.contributions.push(contribution);
    for (const [operation, handler] of contribution.handlers) {
      this.routes.set(`${contribution.extensionPoint}\0${contribution.id}\0${operation}`, handler);
    }
  }

  #send(message) {
    if (!this.stream?.writable) throw new Error('插件 Channel 尚未建立或已经关闭');
    this.stream.write(message);
  }

  #request(requestId, message, timeoutMs) {
    return new Promise((resolveRequest, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        if (this.features.has('channel.cancel.v1')) {
          this.#send({ cancel: { request_id: requestId, reason: 'host call timed out' } });
        }
        reject(new Error('插件请求宿主超时'));
      }, timeoutMs);
      this.pending.set(requestId, (response) => {
        clearTimeout(timer);
        resolveRequest(response);
      });
      this.#send(message);
    });
  }

  async #dispatch(message) {
    if (message.invoke) return this.#invoke(message.invoke);
    if (message.lifecycle) return this.#lifecycle(message.lifecycle);
    if (message.ping) return this.#send({ pong: { request_id: message.ping.request_id } });
    if (message.cancel) {
      this.calls.get(message.cancel.request_id)?.signal.abort(message.cancel.reason);
      return;
    }
    const response = message.host_call_result ?? message.contribution_update_ack;
    if (response?.request_id) {
      const finish = this.pending.get(response.request_id);
      if (finish) {
        this.pending.delete(response.request_id);
        finish(message);
      }
    }
  }

  async #invoke(request) {
    if (!this.active) {
      return this.#replyError(request.request_id, 'plugin.inactive', '插件未激活');
    }
    if ((request.payload?.length ?? 0) > MAX_PAYLOAD_BYTES) {
      return this.#replyError(request.request_id, 'resource.payload_too_large', 'payload 超过协议上限');
    }
    const operation = request.target?.operation ?? '';
    const prefix = `${request.target?.extension_point ?? ''}\0${request.target?.capability ?? ''}\0`;
    const handler = this.routes.get(prefix + operation) ?? this.routes.get(prefix);
    if (!handler) return this.#replyError(request.request_id, 'capability.not_found', '插件未实现目标能力');
    const invocation = new InvocationContext(request);
    this.calls.set(request.request_id, invocation);
    try {
      const output = await this.invocationLocal.run(
        { delegationToken: invocation.delegationToken },
        () => handler(invocation, this, request.context ?? {}, Buffer.from(request.payload ?? [])),
      );
      const payload = Buffer.from(output?.payload ?? []);
      if (payload.length > MAX_PAYLOAD_BYTES) {
        return this.#replyError(request.request_id, 'resource.payload_too_large', '响应 payload 超过协议上限');
      }
      this.#send({ invoke_result: {
        request_id: request.request_id,
        result: output?.result ?? { status: 'STATUS_OK' },
        payload,
      } });
    } catch (error) {
      this.#replyError(request.request_id, 'plugin.handler_error', error.message);
    } finally {
      this.calls.delete(request.request_id);
    }
  }

  #replyError(requestId, code, message) {
    this.#send({ invoke_result: {
      request_id: requestId,
      result: { status: 'STATUS_ERROR', error: { code, message } },
      payload: Buffer.alloc(0),
    } });
  }

  #lifecycle(lifecycle) {
    const operation = lifecycle.op;
    if (operation === 'OP_ACTIVATE') this.active = true;
    if (['OP_DEACTIVATE', 'OP_DRAIN', 'OP_SHUTDOWN'].includes(operation)) this.active = false;
    this.#send({ lifecycle_ack: { request_id: lifecycle.request_id, ready: true } });
    if (operation === 'OP_SHUTDOWN') queueMicrotask(() => void this.shutdown());
  }

  #requireFeature(feature) {
    if (!this.features.has(feature)) throw new Error(`宿主未协商能力 ${feature}`);
  }
}

export const callResult = {
  ok: (payload = Buffer.alloc(0)) => ({ result: { status: 'STATUS_OK' }, payload }),
  error: (code, message) => ({ result: { status: 'STATUS_ERROR', error: { code, message } }, payload: Buffer.alloc(0) }),
};
