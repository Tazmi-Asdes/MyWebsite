import type { APIRoute } from "astro";

function liveResponse(method: string): Response {
  return new Response(method === "HEAD" ? null : JSON.stringify({ status: "live" }), {
    status: 200,
    headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" },
  });
}

export const GET: APIRoute = ({ request }) => liveResponse(request.method);
export const HEAD: APIRoute = ({ request }) => liveResponse(request.method);
