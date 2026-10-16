import type { APIRoute } from "astro";
import { proxyPublicGoRoute } from "../../lib/go-proxy";

async function proxyAsset({ request, params }: Parameters<APIRoute>[0]): Promise<Response> {
  const filename = params.filename ?? "";
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(filename)) {
    return new Response("Not Found", { status: 404, headers: { "Cache-Control": "no-store" } });
  }
  return proxyPublicGoRoute(request);
}

export const GET: APIRoute = proxyAsset;
export const HEAD: APIRoute = proxyAsset;
