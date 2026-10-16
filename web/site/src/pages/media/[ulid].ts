import type { APIRoute } from "astro";
import { proxyPublicGoRoute } from "../../lib/go-proxy";

async function proxyMedia({ request, params }: Parameters<APIRoute>[0]): Promise<Response> {
  if (!/^[0-9A-HJKMNP-TV-Z]{26}$/i.test(params.ulid ?? "")) {
    return new Response("Not Found", { status: 404, headers: { "Cache-Control": "no-store" } });
  }
  return proxyPublicGoRoute(request);
}

export const GET: APIRoute = proxyMedia;
export const HEAD: APIRoute = proxyMedia;
