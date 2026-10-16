import {
  fetchPublicData,
  parseHomePage,
  type HomePageData,
} from "./public-data";

export type {
  HomeArticle,
  HomePageData,
  HomeProfile,
  HomeProject,
  HomeSite,
} from "./public-data";

export class HomeDataUnavailableError extends Error {
  constructor() {
    super("公开首页数据暂时无法读取。");
    this.name = "HomeDataUnavailableError";
  }
}

export async function fetchHomePageData(): Promise<HomePageData> {
  try {
    return await fetchPublicData("/api/public/v1/home", parseHomePage);
  } catch {
    throw new HomeDataUnavailableError();
  }
}
