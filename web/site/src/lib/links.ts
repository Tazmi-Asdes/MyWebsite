import type { HomeArticle, HomeProject } from "./home-data";

const ULID = "[0-9A-HJKMNP-TV-Z]{26}";
const INTERNAL_PATHS = new RegExp(`^/(?:|articles(?:/${ULID})?|projects|about)$`, "i");

export interface ProjectDestination {
  href: string;
  external: boolean;
}

export function articleHref(article: HomeArticle): string {
  return /^\/articles(?:\/[0-9A-HJKMNP-TV-Z]{26})?$/i.test(article.url) ? article.url : "/articles";
}

export function projectDestination(project: HomeProject): ProjectDestination {
  if (project.external) {
    const candidate = project.github_url.trim() || project.url.trim();
    try {
      const url = new URL(candidate);
      if ((url.protocol === "https:" || url.protocol === "http:") && !url.username && !url.password) {
        return { href: url.toString(), external: true };
      }
    } catch {
      return { href: "/projects", external: false };
    }
    return { href: "/projects", external: false };
  }

  const candidate = project.url.trim();
  return INTERNAL_PATHS.test(candidate)
    ? { href: candidate, external: false }
    : { href: "/projects", external: false };
}

export function contactDestination(contact: { href: string; external: boolean }): ProjectDestination {
  const candidate = contact.href.trim();
  if (contact.external) {
    try {
      const url = new URL(candidate);
      if ((url.protocol === "https:" || url.protocol === "http:") && !url.username && !url.password) {
        return { href: url.toString(), external: true };
      }
    } catch {
      return { href: "/about", external: false };
    }
    return { href: "/about", external: false };
  }

  if (candidate.startsWith("mailto:") && !/[\r\n]/.test(candidate)) {
    return { href: candidate, external: false };
  }
  return INTERNAL_PATHS.test(candidate) ? { href: candidate, external: false } : { href: "/about", external: false };
}
