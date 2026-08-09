import { createApp, ref, watchEffect } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './styles/main.scss'

const mql = window.matchMedia('(prefers-color-scheme: dark)')
const isDark = ref(mql.matches)

watchEffect(() => {
  document.documentElement.dataset.colorScheme = isDark.value ? 'dark' : 'light'
})

mql.addEventListener('change', (e) => {
  isDark.value = e.matches
})

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
