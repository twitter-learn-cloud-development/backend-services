import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { AuthSession, type SessionUser } from '../utils/authSession'

export const useUserStore = defineStore('user', () => {
    const session = new AuthSession(localStorage)
    const data = ref(session.data)
    const revision = ref(session.revision)
    const token = computed(() => data.value.token)
    const user = computed(() => data.value.user)
    const sync = () => { data.value = session.data; revision.value = session.revision }
    function beginLogin() { const epoch = session.begin(); sync(); return epoch }
    function isCurrent(epoch: number) { return session.current(epoch) }
    function reload() { session.reload(); sync() }
    function completeLogin(epoch: number, token: string, user: SessionUser) {
        try { return session.commit(epoch, token, user) } finally { sync() }
    }
    function unauthorized(epoch: number, authorization: string) {
        try { return session.unauthorized(epoch, authorization) } finally { sync() }
    }

    function logout() {
        try { session.clear() } finally { sync() }
    }

    return { token, user, revision, beginLogin, isCurrent, completeLogin, unauthorized, logout, reload }
})
