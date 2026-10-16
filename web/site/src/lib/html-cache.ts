import { createHash } from "node:crypto";

const PUBLIC_HTML_PATHS = [
  /^\/$/,
  /^\/articles$/,
  /^\/articles\/[0-9A-HJKMNP-TV-Z]{26}$/i,
  /^\/projects$/,
  /^\/about$/,
];

function publicHtmlPath(path: string): boolean {
  return PUBLIC_HTML_PATHS.some((pattern) => pattern.test(path));
}

function mergeVary(headers: Headers): void {
  const current = headers.get("Vary");
  if (current?.trim() === "*") return;
  const values = new Map<string, string>();
  for (const value of current?.split(",") ?? []) {
    const trimmed = value.trim();
    if (trimmed) values.set(trimmed.toLowerCase(), trimmed);
  }
  values.set("cookie", "Cookie");
  values.set("authorization", "Authorization");
  headers.set("Vary", [...values.values()].join(", "));
}

function withNoStore(response: Response, method: string): Response {
  const headers = new Headers(response.headers);
  headers.set("Cache-Control", "no-store");
  mergeVary(headers);
  if (method === "HEAD") {
    return new Response(null, { status: response.status, statusText: response.statusText, headers });
  }
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers });
}

function ifNoneMatchMatches(header: string | null, etag: string): boolean {
  if (!header) return false;
  return header.split(",").some((candidate) => {
    const token = candidate.trim();
    return token === "*" || token.replace(/^W\//i, "") === etag;
  });
}

export async function applyPublicHtmlCache(request: Request, response: Response): Promise<Response> {
  const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
  if (!contentType.includes("text/html")) return response;

  const pathname = new URL(request.url).pathname;
  const canCache = request.method === "GET" &&
    publicHtmlPath(pathname) &&
    response.status === 200 &&
    !request.headers.has("cookie") &&
    !request.headers.has("authorization") &&
    !response.headers.has("set-cookie");

  if (!canCache) return withNoStore(response, request.method);

  const body = await response.arrayBuffer();
  const digest = createHash("sha256").update(new Uint8Array(body)).digest("base64url");
  const etag = `"sha256-${digest}"`;
  const headers = new Headers(response.headers);
  mergeVary(headers);
  headers.set("ETag", etag);
  headers.set("Cache-Control", "public, max-age=0, must-revalidate");

  if (ifNoneMatchMatches(request.headers.get("if-none-match"), etag)) {
    headers.delete("Content-Length");
    return new Response(null, { status: 304, statusText: "Not Modified", headers });
  }

  return new Response(body, { status: response.status, statusText: response.statusText, headers });
}
