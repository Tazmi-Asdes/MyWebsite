import type { APIRoute } from "astro";
import { proxyLegacyHomeRoute } from "../../lib/go-proxy";

export const prerender = false;

export const GET: APIRoute = ({ request }) => proxyLegacyHomeRoute(request);
export const HEAD: APIRoute = ({ request }) => proxyLegacyHomeRoute(request);
