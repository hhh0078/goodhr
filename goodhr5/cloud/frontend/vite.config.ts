import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [vue(), officialLayoutPlugin(), staticCacheHeadersPlugin(), adminHistoryFallbackPlugin()],
  server: {
    allowedHosts: ['goodhr5.58it.cn', '127.0.0.1', 'localhost'],
    hmr: {
      host: '127.0.0.1',
      clientPort: 5173,
      protocol: 'ws',
    },
  },
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        features: resolve(__dirname, 'features.html'),
        videos: resolve(__dirname, 'videos.html'),
        pricing: resolve(__dirname, 'pricing.html'),
        contact: resolve(__dirname, 'contact.html'),
        admin: resolve(__dirname, 'admin/index.html')
      },
      output: {
        entryFileNames: 'assets/[name].js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]'
      }
    }
  },
  define: {
    __VUE_OPTIONS_API__: false,
    __VUE_PROD_DEVTOOLS__: false
  }
})

const officialNavItems = [
  { href: '/', label: '首页', match: ['', 'index.html'] },
  { href: '/features.html', label: '功能介绍', match: ['features.html'] },
  { href: '/videos.html', label: '安装视频教程', match: ['videos.html'] },
  { href: '/pricing.html', label: '产品定价', match: ['pricing.html'] },
  { href: '/contact.html', label: '联系我们', match: ['contact.html'] },
]

/**
 * 在构建阶段注入官网公共导航和底部，避免页面依赖 JS 才能显示导航。
 * @returns {any} Vite 插件配置。
 */
function officialLayoutPlugin() {
  return {
    name: 'goodhr-official-layout',
    transformIndexHtml(html: string, context: any) {
      if (!html.includes('data-site-header') && !html.includes('data-site-footer')) {
        return html
      }
      const pageName = officialPageName(context?.path || '')
      return html
        .replace('<div data-site-header></div>', buildOfficialHeader(pageName))
        .replace('<div data-site-footer></div>', buildOfficialFooter())
    },
  }
}

/**
 * 生成官网公共导航栏 HTML。
 * @param {string} pageName - 当前页面文件名。
 * @returns {string} 导航栏 HTML。
 */
function buildOfficialHeader(pageName: string) {
  const navHTML = officialNavItems
    .map((item) => {
      const active = item.match.includes(pageName) ? ' class="active"' : ''
      return `<a${active} href="${item.href}">${item.label}</a>`
    })
    .join('')
  return [
    '<header class="site-header">',
    '<a class="brand" href="/">GoodHR</a>',
    `<nav>${navHTML}</nav>`,
    '<div class="nav-stats" data-public-stats>',
    '<span>已处理 <strong data-stat-field="processed_resume_count">--</strong> 份简历</span>',
    '<span>今日新注册 <strong data-stat-field="today_registered_count">--</strong></span>',
    '</div>',
    '<a class="admin-link" href="/admin/">进入后台</a>',
    '</header>',
  ].join('')
}

/**
 * 生成官网公共底部 HTML。
 * @returns {string} 底部 HTML。
 */
function buildOfficialFooter() {
  return [
    '<footer class="site-footer">',
    '<span>GoodHR 招聘自动化工具</span>',
    '<span>联系：17607080935</span>',
    '</footer>',
  ].join('')
}

/**
 * 根据 Vite 当前路径返回官网页面文件名。
 * @param {string} path - 当前页面路径。
 * @returns {string} 页面文件名。
 */
function officialPageName(path: string) {
  const pathname = path.split('?')[0].replace(/\/+$/, '')
  return pathname.split('/').pop() || ''
}

/**
 * 给预览服务增加缓存头，避免部署后旧 HTML 引用已删除的构建文件。
 * @returns {any} Vite 插件配置。
 */
function staticCacheHeadersPlugin() {
  const applyHeaders = (req: any, res: any, next: any) => {
    const url = String(req.url || '').split('?')[0]
    if (url.endsWith('.html') || url === '/' || url.startsWith('/admin/')) {
      res.setHeader('Cache-Control', 'no-store, max-age=0, must-revalidate')
    } else if (url.startsWith('/assets/')) {
      res.setHeader('Cache-Control', 'no-cache, max-age=0, must-revalidate')
    }
    next()
  }
  return {
    name: 'goodhr-static-cache-headers',
    configureServer(server: any) {
      server.middlewares.use(applyHeaders)
    },
    configurePreviewServer(server: any) {
      server.middlewares.use(applyHeaders)
    },
  }
}

/**
 * 为后台子路由提供 HTML 回退，避免刷新 /admin/accounts 时进入官网首页。
 * @returns {any} Vite 插件配置。
 */
function adminHistoryFallbackPlugin() {
  return {
    name: 'goodhr-admin-history-fallback',
    configureServer(server: any) {
      server.middlewares.use(async (req: any, res: any, next: any) => {
        if (!isAdminPageRequest(req)) {
          next()
          return
        }
        const htmlPath = resolve(__dirname, 'admin/index.html')
        const rawHtml = readFileSync(htmlPath, 'utf-8')
        const html = await server.transformIndexHtml(req.originalUrl || req.url || '/admin/', rawHtml)
        res.statusCode = 200
        res.setHeader('Content-Type', 'text/html')
        res.end(html)
      })
    },
    configurePreviewServer(server: any) {
      server.middlewares.use((req: any, res: any, next: any) => {
        if (!isAdminPageRequest(req)) {
          next()
          return
        }
        const htmlPath = resolve(__dirname, 'dist/admin/index.html')
        if (!existsSync(htmlPath)) {
          next()
          return
        }
        res.statusCode = 200
        res.setHeader('Content-Type', 'text/html')
        res.end(readFileSync(htmlPath, 'utf-8'))
      })
    },
  }
}

/**
 * 判断当前请求是否是后台页面路由，而不是后台静态资源。
 * @param {any} req - Vite 中间件请求对象。
 * @returns {boolean} 是否需要返回后台入口 HTML。
 */
function isAdminPageRequest(req: any) {
  if (req.method && req.method !== 'GET') return false
  const url = String(req.url || '').split('?')[0]
  if (!url.startsWith('/admin/')) return false
  if (url === '/admin/' || url.endsWith('.html')) return false
  return !url.split('/').pop()?.includes('.')
}
