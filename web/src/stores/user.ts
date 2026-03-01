import { defineStore } from 'pinia'
import { ref } from 'vue'
import router from '../router'

export const useUserStore = defineStore('user', () => {
    const token = ref(localStorage.getItem('token') || '')
    const user = ref<any>(JSON.parse(localStorage.getItem('user') || 'null'))

    function setToken(newToken: string) {
        token.value = newToken
        localStorage.setItem('token', newToken)
    }

    function setUser(newUser: any) {
        user.value = newUser
        localStorage.setItem('user', JSON.stringify(newUser))
    }

    function logout() {
        token.value = ''
        user.value = null
        localStorage.removeItem('token')
        localStorage.removeItem('user')
        router.push('/login')
    }

    return { token, user, setToken, setUser, logout }
})
