// GoodHR 5 云端前端入口
import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './styles/terminal.css'
import './styles/themes.css'
import { applyTheme, loadCachedTheme } from './services/theme'

const cachedTheme = loadCachedTheme()
if (cachedTheme) applyTheme(cachedTheme)

createApp(App).use(router).mount('#app')
