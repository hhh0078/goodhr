/** 本文件负责给动态页面响应补充禁止缓存头，避免旧 HTML 引用已删除的 Next.js chunk。 */
import { NextResponse, type NextRequest } from "next/server";

const NO_STORE = "no-store, no-cache, must-revalidate, proxy-revalidate";

/** proxy 为非静态资源页面统一设置 no-store 缓存头。 */
export function proxy(request: NextRequest) {
  const response = NextResponse.next();
  const pathname = request.nextUrl.pathname;

  if (shouldDisablePageCache(pathname)) {
    response.headers.set("Cache-Control", NO_STORE);
    response.headers.set("Pragma", "no-cache");
    response.headers.set("Expires", "0");
  }

  return response;
}

/** shouldDisablePageCache 判断当前路径是否属于页面请求。 */
function shouldDisablePageCache(pathname: string) {
  if (
    pathname.startsWith("/_next/") ||
    pathname.startsWith("/api/") ||
    pathname.startsWith("/downloads/")
  ) {
    return false;
  }

  return !/\.[a-z0-9]+$/i.test(pathname);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|robots.txt|sitemap.xml|llms.txt).*)"],
};
