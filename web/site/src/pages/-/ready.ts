import type { APIRoute } from "astro";
import { fetchPublicData, parseHomePage } from "../../lib/public-data";

async function readyResponse(method: string): Promise<Response> {
  let status = 200;
  let payload = { status: "ready" };
  try {
    await fetchPublicData("/api/public/v1/home", parseHomePage);
  } catch {
    status = 503;
    payload = { status: "not_ready" };
  }

  return new Response(method === "HEAD" ? null : JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" },
  });
}

export const GET: APIRoute = ({ request }) => readyResponse(request.method);
export const HEAD: APIRoute = ({ request }) => readyResponse(request.method);
