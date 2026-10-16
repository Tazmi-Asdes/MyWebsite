import assert from "node:assert/strict";
import { test } from "node:test";
import { applyPublicHtmlCache } from "./html-cache.ts";

const url = "http://localhost/articles?page=1";

function htmlResponse(body: string, status = 200, headers: HeadersInit = {}): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "text/html; charset=utf-8", ...headers },
  });
}

test("public HTML uses a content ETag and weak or wildcard validators return bodyless 304", async () => {
  const first = await applyPublicHtmlCache(new Request(url), htmlResponse("<main>one</main>"));
  const etag = first.headers.get("ETag");
  assert.ok(etag);
  assert.equal(first.headers.get("Cache-Control"), "public, max-age=0, must-revalidate");
  assert.equal(first.headers.get("Vary"), "Cookie, Authorization");

  for (const validator of [`W/${etag}`, "*"]) {
    const response = await applyPublicHtmlCache(
      new Request(url, { headers: { "If-None-Match": validator } }),
      htmlResponse("<main>one</main>"),
    );
    assert.equal(response.status, 304);
    assert.equal(response.body, null);
    assert.equal(response.headers.get("ETag"), etag);
  }
});

test("changed HTML content receives a new ETag and does not match the prior validator", async () => {
  const first = await applyPublicHtmlCache(new Request(url), htmlResponse("<main>one</main>"));
  const priorTag = first.headers.get("ETag");
  const changed = await applyPublicHtmlCache(
    new Request(url, { headers: { "If-None-Match": priorTag ?? "" } }),
    htmlResponse("<main>two</main>"),
  );
  assert.equal(changed.status, 200);
  assert.notEqual(changed.headers.get("ETag"), priorTag);
  assert.equal(await changed.text(), "<main>two</main>");
});

test("error, credentialed, personalized, and non-public HTML stays no-store", async () => {
  const cases: Array<[Request, Response]> = [
    [new Request(url), htmlResponse("missing", 404)],
    [new Request(url), htmlResponse("unavailable", 503)],
    [new Request(url, { headers: { Cookie: "session=example" } }), htmlResponse("private")],
    [new Request(url, { headers: { Authorization: "Bearer example" } }), htmlResponse("private")],
    [new Request(url), htmlResponse("personalized", 200, { "Set-Cookie": "session=example" })],
    [new Request("http://localhost/legacy/"), htmlResponse("legacy")],
  ];

  for (const [request, response] of cases) {
    const result = await applyPublicHtmlCache(request, response);
    assert.equal(result.headers.get("Cache-Control"), "no-store");
    assert.equal(result.headers.get("Vary"), "Cookie, Authorization");
  }
});

test("HEAD HTML responses are bodyless and are not stored", async () => {
  const response = await applyPublicHtmlCache(new Request(url, { method: "HEAD" }), htmlResponse("ignored"));
  assert.equal(response.status, 200);
  assert.equal(response.body, null);
  assert.equal(response.headers.get("Cache-Control"), "no-store");
});
