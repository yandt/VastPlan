import { resolve } from "node:path";
import protobuf from "protobufjs";
import type { CallContext, CallResult, CallTarget } from "./types.js";

interface DecodedInvokeResponse {
  request_id?: string;
  result?: CallResult;
  payload?: Uint8Array;
  transport_error?: { code?: string; message?: string; retryable?: boolean };
}

interface DecodedInvokeRequest {
  request_id?: string;
  target?: CallTarget;
  context?: CallContext;
  payload?: Uint8Array;
}

/** Loads the repository's language-neutral Addressing v1 Protobuf contract. */
export class AddressingProtocolCodec {
  private readonly invokeRequest: protobuf.Type;
  private readonly invokeResponse: protobuf.Type;
  private readonly callContext: protobuf.Type;

  public constructor(contractsDirectory: string) {
    const root = new protobuf.Root();
    root.resolvePath = (_origin, target) => resolve(contractsDirectory, target);
    root.loadSync(resolve(contractsDirectory, "addressing/v1/addressing.proto"), { keepCase: true });
    root.resolveAll();
    this.invokeRequest = root.lookupType("vastplan.addressing.v1.InvokeRequest");
    this.invokeResponse = root.lookupType("vastplan.addressing.v1.InvokeResponse");
    this.callContext = root.lookupType("vastplan.contract.v1.CallContext");
  }

  public encodeRequest(requestID: string, target: CallTarget, context: CallContext, payload: Uint8Array): Uint8Array {
    const value = { request_id: requestID, target, context, payload };
    const error = this.invokeRequest.verify(value);
    if (error !== null) throw new Error(`Addressing InvokeRequest 无效: ${error}`);
    return this.invokeRequest.encode(value).finish();
  }

  public decodeResponse(bytes: Uint8Array): DecodedInvokeResponse {
    const decoded = this.invokeResponse.decode(bytes);
    return this.invokeResponse.toObject(decoded, { longs: String, enums: Number, bytes: Uint8Array, defaults: false }) as DecodedInvokeResponse;
  }

  public decodeRequest(bytes: Uint8Array): DecodedInvokeRequest {
    const decoded = this.invokeRequest.decode(bytes);
    return this.invokeRequest.toObject(decoded, { longs: String, enums: Number, bytes: Uint8Array, defaults: false }) as DecodedInvokeRequest;
  }

  public encodeResponse(requestID: string, result: CallResult | undefined, payload: Uint8Array, transportError?: { code: string; message: string; retryable?: boolean }): Uint8Array {
    const value = { request_id: requestID, ...(result === undefined ? {} : { result }), payload, ...(transportError === undefined ? {} : { transport_error: transportError }) };
    const error = this.invokeResponse.verify(value);
    if (error !== null) throw new Error(`Addressing InvokeResponse 无效: ${error}`);
    return this.invokeResponse.encode(value).finish();
  }

  public contextSize(context: CallContext): number {
    const error = this.callContext.verify(context);
    if (error !== null) throw new Error(`Addressing CallContext 无效: ${error}`);
    return this.callContext.encode(context).finish().byteLength;
  }
}
