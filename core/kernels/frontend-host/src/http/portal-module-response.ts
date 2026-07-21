import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalDeliveryObject } from "../runtime/portal-delivery-store";

export function sendPortalModule(request: IncomingMessage, response: ServerResponse, object: PortalDeliveryObject): void {
  const mediaType = object.descriptor.mediaType;
  const contentType = mediaType.startsWith("text/") || mediaType === "application/json" ? `${mediaType}; charset=utf-8` : mediaType;
  const etag = `"sha256-${object.descriptor.sha256}"`;
  response.setHeader("Content-Type", contentType);
  response.setHeader("Cache-Control", "private, max-age=31536000, immutable");
  response.setHeader("Cross-Origin-Resource-Policy", "same-origin");
  response.setHeader("X-VastPlan-Module-SHA256", object.descriptor.sha256);
  response.setHeader("X-VastPlan-Package-SHA256", object.descriptor.packageSha256);
  response.setHeader("ETag", etag);
  if (request.headers["if-none-match"] === etag) {
    response.statusCode = 304;
    response.end();
    return;
  }
  const gzip = object.gzipContent !== undefined && acceptsGzip(request.headers["accept-encoding"]);
  if (gzip) {
    response.setHeader("Content-Encoding", "gzip");
    response.setHeader("Vary", "Accept-Encoding");
  }
  response.statusCode = 200;
  response.end(request.method === "HEAD" ? undefined : gzip ? object.gzipContent : object.content);
}

function acceptsGzip(value: string | undefined): boolean {
  return (value ?? "").split(",").some((item) => item.split(";", 1)[0]?.trim() === "gzip");
}
