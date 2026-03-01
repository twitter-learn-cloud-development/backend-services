import axios from 'axios'
import { useUserStore } from '../stores/user'

const request = axios.create({
    baseURL: '/api/v1',
    timeout: 5000
})

// Request Interceptor
request.interceptors.request.use(
    (config) => {
        const userStore = useUserStore()
        if (userStore.token) {
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
            userStore.logout()
        }
        return Promise.reject(error)
    }
)

export default request
