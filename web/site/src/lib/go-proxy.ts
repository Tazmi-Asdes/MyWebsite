import { goPublicOrigin } from "./go-origin";

const ULID = "[0-9A-HJKMNP-TV-Z]{26}";
const ALLOWED_PUBLIC_PATHS = [
  new RegExp(`^/media/${ULID}$`, "i"),
  /^\/assets\/[A-Za-z0-9][A-Za-z0-9._-]*$/,
];
const FORWARDED_REQUEST_HEADERS = ["accept", "range", "if-range", "if-none-match", "if-modified-since"];
const FORWARDED_RESPONSE_HEADERS = [
  "accept-ranges",
  "cache-control",
  "content-disposition",
  "content-range",
  "content-type",
  "etag",
  "expires",
  "last-modified",
  "location",
  "vary",
];
const TIMEOUT_MS = 4_500;

export function isPublicGoPath(path: string): boolean {
  return ALLOWED_PUBLIC_PATHS.some((pattern) => pattern.test(path));
}

export async function proxyPublicGoRoute(request: Request): Promise<Response> {
  const incoming = new URL(request.url);
  if (!isPublicGoPath(incoming.pathname)) {
    return new Response("Not Found", {
      status: 404,
      headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store" },
    });
  }

  return proxyGoPath(request, `${incoming.pathname}${incoming.search}`);
}

export async function proxyLegacyHomeRoute(request: Request): Promise<Response> {
  if (process.env.NODE_ENV === "production") {
    return new Response("Not Found", {
      status: 404,
      headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store" },
    });
  }

  const response = await proxyGoPath(request, "/");
  const headers = new Headers(response.headers);
  headers.set("Cache-Control", "no-store");
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}

async function proxyGoPath(request: Request, pathAndQuery: string): Promise<Response> {
  try {
    const headers = new Headers();
    for (const name of FORWARDED_REQUEST_HEADERS) {
      const value = request.headers.get(name);
      if (value) headers.set(name, value);
    }

    const upstream = await fetch(new URL(pathAndQuery, goPublicOrigin()), {
      method: request.method,
      headers,
      signal: AbortSignal.timeout(TIMEOUT_MS),
      cache: "no-store",
      redirect: "manual",
    });
    const responseHeaders = new Headers();

    for (const name of FORWARDED_RESPONSE_HEADERS) {
      const value = upstream.headers.get(name);
      if (!value) continue;
      if (name === "location" && (!value.startsWith("/") || value.startsWith("//") || value.includes("\\"))) continue;
      responseHeaders.set(name, value);
    }

    return new Response(request.method === "HEAD" ? null : upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: responseHeaders,
    });
  } catch {
    return new Response(
      "<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>页面暂时无法访问</title><body><main><h1>页面暂时无法访问</h1><p>公开内容服务暂时不可用，请稍后再试。</p><a href=\"/\">返回首页</a></main></body></html>",
      {
        status: 503,
        headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" },
      },
    );
  }
}
