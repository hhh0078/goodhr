/// <reference types="vite/client" />
declare module '*.vue' { import type { DefineComponent } from 'vue'; const c: DefineComponent; export default c }

interface Window {
  GOODHR_CLOUD_API?: string
}
