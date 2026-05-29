import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [vue(), adminHistoryFallbackPlugin()],
  server: {
    allowedHosts: ['goodhr5.58it.cn']
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
      }
    }
  },
  define: {
    __VUE_OPTIONS_API__: false,
    __VUE_PROD_DEVTOOLS__: false
  }
})

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
