import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { restoreColorSchemePreference } from './sdk/colorScheme'
import './styles/main.scss'

restoreColorSchemePreference()

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
