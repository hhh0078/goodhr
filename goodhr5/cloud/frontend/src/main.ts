// GoodHR 5 云端前端入口
import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './styles/terminal.css'

createApp(App).use(router).mount('#app')
