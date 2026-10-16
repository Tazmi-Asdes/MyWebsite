import { goPublicOrigin } from "./go-origin";

export interface HomeSite {
  name: string;
  description: string;
  eyebrow: string;
}

export interface HomeProfile {
  name: string;
  avatar_url: string;
  tagline: string;
}

export interface HomeArticle {
  title: string;
  preview: string;
  published: string;
  date_time: string;
  url: string;
}

export interface HomeProject {
  name: string;
  image_url: string;
  image_alt: string;
  github_url: string;
  url: string;
  external: boolean;
}

export interface HomePageData {
  site: HomeSite;
  profile: HomeProfile;
  year: number;
  articles: HomeArticle[];
  projects: HomeProject[];
}

export interface PageMetadata {
  title: string;
  description: string;
  heading: string;
  eyebrow: string;
}

export interface PublicPageBase {
  site: HomeSite;
  year: number;
  page: PageMetadata;
}

export interface ArticlePagination {
  page: number;
  total_pages: number;
  previous_page: number;
  next_page: number;
  has_previous: boolean;
  has_next: boolean;
}

export interface ArticleListPageData extends PublicPageBase {
  articles: HomeArticle[];
  pagination: ArticlePagination;
}

export interface ProjectsPageData extends PublicPageBase {
  projects: HomeProject[];
}

export interface ArticleTocItem {
  id: string;
  text: string;
  level: number;
}

export interface ArticleDetail extends HomeArticle {
  body_html: string;
  toc: ArticleTocItem[];
}

export interface ArticleDetailPageData extends PublicPageBase {
  article: ArticleDetail;
}

export interface PublicContact {
  label: string;
  href: string;
  external: boolean;
}

export interface AboutPageData extends PublicPageBase {
  profile: HomeProfile;
  article_count: number;
  project_count: number;
  interests: string[];
  contacts: PublicContact[];
}

export class PublicApiError extends Error {
  constructor(
    readonly status: 404 | 503,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "PublicApiError";
  }
}

const TIMEOUT_MS = 4_500;
const UNAVAILABLE = () => new PublicApiError(503, "public_api_unavailable", "公开内容服务暂时无法读取，请稍后重试。");

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function invalidData(): never {
  throw new PublicApiError(503, "invalid_public_response", "公开内容暂时无法读取，请稍后重试。");
}

export function requiredString(record: Record<string, unknown>, field: string): string {
  const value = record[field];
  if (typeof value !== "string") return invalidData();
  return value;
}

function requiredNumber(record: Record<string, unknown>, field: string): number {
  const value = record[field];
  if (typeof value !== "number" || !Number.isInteger(value)) return invalidData();
  return value;
}

function requiredBoolean(record: Record<string, unknown>, field: string): boolean {
  const value = record[field];
  if (typeof value !== "boolean") return invalidData();
  return value;
}

function requiredArray(value: unknown): unknown[] {
  if (!Array.isArray(value)) return invalidData();
  return value;
}

function parseSite(value: unknown): HomeSite {
  if (!isRecord(value)) return invalidData();
  return {
    name: requiredString(value, "name"),
    description: requiredString(value, "description"),
    eyebrow: requiredString(value, "eyebrow"),
  };
}

function parseProfile(value: unknown): HomeProfile {
  if (!isRecord(value)) return invalidData();
  return {
    name: requiredString(value, "name"),
    avatar_url: requiredString(value, "avatar_url"),
    tagline: requiredString(value, "tagline"),
  };
}

export function parseArticle(value: unknown): HomeArticle {
  if (!isRecord(value)) return invalidData();
  return {
    title: requiredString(value, "title"),
    preview: requiredString(value, "preview"),
    published: requiredString(value, "published"),
    date_time: requiredString(value, "date_time"),
    url: requiredString(value, "url"),
  };
}

export function parseProject(value: unknown): HomeProject {
  if (!isRecord(value)) return invalidData();
  return {
    name: requiredString(value, "name"),
    image_url: requiredString(value, "image_url"),
    image_alt: requiredString(value, "image_alt"),
    github_url: requiredString(value, "github_url"),
    url: requiredString(value, "url"),
    external: requiredBoolean(value, "external"),
  };
}

function parsePageMetadata(value: unknown): PageMetadata {
  if (!isRecord(value)) return invalidData();
  return {
    title: requiredString(value, "title"),
    description: requiredString(value, "description"),
    heading: requiredString(value, "heading"),
    eyebrow: requiredString(value, "eyebrow"),
  };
}

function parsePageBase(value: unknown): PublicPageBase {
  if (!isRecord(value)) return invalidData();
  return {
    site: parseSite(value.site),
    year: requiredNumber(value, "year"),
    page: parsePageMetadata(value.page),
  };
}

export function parseHomePage(value: unknown): HomePageData {
  if (!isRecord(value)) return invalidData();
  return {
    site: parseSite(value.site),
    profile: parseProfile(value.profile),
    year: requiredNumber(value, "year"),
    articles: requiredArray(value.articles).map(parseArticle).slice(0, 3),
    projects: requiredArray(value.projects).map(parseProject).slice(0, 3),
  };
}

export function parseArticleListPage(value: unknown): ArticleListPageData {
  if (!isRecord(value) || !isRecord(value.pagination)) return invalidData();
  const base = parsePageBase(value);
  return {
    ...base,
    articles: requiredArray(value.articles).map(parseArticle),
    pagination: {
      page: requiredNumber(value.pagination, "page"),
      total_pages: requiredNumber(value.pagination, "total_pages"),
      previous_page: requiredNumber(value.pagination, "previous_page"),
      next_page: requiredNumber(value.pagination, "next_page"),
      has_previous: requiredBoolean(value.pagination, "has_previous"),
      has_next: requiredBoolean(value.pagination, "has_next"),
    },
  };
}

export function parseProjectsPage(value: unknown): ProjectsPageData {
  if (!isRecord(value)) return invalidData();
  return {
    ...parsePageBase(value),
    projects: requiredArray(value.projects).map(parseProject),
  };
}

export function parseArticleDetailPage(value: unknown): ArticleDetailPageData {
  if (!isRecord(value) || !isRecord(value.article)) return invalidData();
  const articleBase = parseArticle(value.article);
  const toc = requiredArray(value.article.toc).map((item): ArticleTocItem => {
    if (!isRecord(item)) return invalidData();
    return {
      id: requiredString(item, "id"),
      text: requiredString(item, "text"),
      level: requiredNumber(item, "level"),
    };
  });
  return {
    ...parsePageBase(value),
    article: {
      ...articleBase,
      body_html: requiredString(value.article, "body_html"),
      toc,
    },
  };
}

export function parseAboutPage(value: unknown): AboutPageData {
  if (!isRecord(value)) return invalidData();
  return {
    ...parsePageBase(value),
    profile: parseProfile(value.profile),
    article_count: requiredNumber(value, "article_count"),
    project_count: requiredNumber(value, "project_count"),
    interests: requiredArray(value.interests).map((item) => {
      if (typeof item !== "string") return invalidData();
      return item;
    }),
    contacts: requiredArray(value.contacts).map((item): PublicContact => {
      if (!isRecord(item)) return invalidData();
      return {
        label: requiredString(item, "label"),
        href: requiredString(item, "href"),
        external: requiredBoolean(item, "external"),
      };
    }),
  };
}

function parseApiError(status: number, value: unknown): PublicApiError {
  if ((status === 404 || status === 503) && isRecord(value)) {
    const code = value.code;
    const message = value.message;
    if (typeof code === "string" && typeof message === "string") {
      return new PublicApiError(status, code, message);
    }
  }
  return UNAVAILABLE();
}

export async function fetchPublicData<T>(path: string, parse: (value: unknown) => T): Promise<T> {
  try {
    const response = await fetch(new URL(path, goPublicOrigin()), {
      headers: { Accept: "application/json" },
      signal: AbortSignal.timeout(TIMEOUT_MS),
      cache: "no-store",
      redirect: "error",
    });
    if (!response.headers.get("content-type")?.toLowerCase().includes("application/json")) {
      throw UNAVAILABLE();
    }
    const value: unknown = await response.json();
    if (!response.ok) throw parseApiError(response.status, value);
    return parse(value);
  } catch (error) {
    if (error instanceof PublicApiError) throw error;
    throw UNAVAILABLE();
  }
}
