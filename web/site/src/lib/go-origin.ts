const DEFAULT_GO_PUBLIC_ORIGIN = "http://127.0.0.1:18094";

export function goPublicOrigin(): URL {
  const configured = process.env.GO_PUBLIC_ORIGIN?.trim() || DEFAULT_GO_PUBLIC_ORIGIN;

  try {
    const url = new URL(configured);
    if (
      (url.protocol !== "http:" && url.protocol !== "https:") ||
      url.username !== "" ||
      url.password !== "" ||
      (url.pathname !== "" && url.pathname !== "/") ||
      url.search !== "" ||
      url.hash !== ""
    ) {
      throw new Error("Invalid Go public origin.");
    }
    return url;
  } catch {
    throw new Error("Go public origin is unavailable.");
  }
}
