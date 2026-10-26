import { createApp } from 'vue'
import App from './App.vue'
import WidgetView from './WidgetView.vue'
import './styles.css'

createApp(new URLSearchParams(location.search).get('embed') === '1' ? WidgetView : App).mount('#app')
