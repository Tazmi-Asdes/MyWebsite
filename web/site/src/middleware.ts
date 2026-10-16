import { defineMiddleware } from "astro:middleware";
import { applyPublicHtmlCache } from "./lib/html-cache";

export const onRequest = defineMiddleware(async (context, next) => {
  const response = await next();
  return applyPublicHtmlCache(context.request, response);
});
