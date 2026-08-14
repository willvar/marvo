import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { restoreColorSchemePreference } from './sdk/colorScheme'
import { installAppNavigation } from './sdk/appBack'
import './styles/main.scss'

restoreColorSchemePreference()
installAppNavigation(router)

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
