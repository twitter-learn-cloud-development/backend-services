import axios from 'axios'
import { useUserStore } from '../stores/user'

declare module 'axios' { interface InternalAxiosRequestConfig { authRevision?: number } }

const request = axios.create({
    baseURL: '/api/v1',
    timeout: 5000
})

// Request Interceptor
request.interceptors.request.use(
    (config) => {
        const userStore = useUserStore()
        config.authRevision = userStore.revision
        if (userStore.token && !['/auth/login', '/auth/register'].includes(config.url || '')) {
            config.headers.Authorization = `Bearer ${userStore.token}`
        }
        return config
    },
    (error) => {
        return Promise.reject(error)
    }
)

// Response Interceptor
request.interceptors.response.use(
    (response) => {
        return response
    },
    (error) => {
        if (error.response && error.response.status === 401) {
            const userStore = useUserStore()
            userStore.unauthorized(error.config?.authRevision, String(error.config?.headers?.Authorization || ''))
        }
        return Promise.reject(error)
    }
)

export default request
