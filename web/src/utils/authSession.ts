export interface SessionUser { id: string; username?: string; email?: string; [key: string]: any }
export interface SessionData { token: string; user: SessionUser | null }
type SessionStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

// A revision belongs to a client session, including pending sign-in attempts.
export class AuthSession {
    revision = 0
    data: SessionData = { token: '', user: null }
    private storage: SessionStorage
    constructor(storage: SessionStorage) {
        this.storage = storage
        try {
            const token = storage.getItem('token') || ''
            const user = JSON.parse(storage.getItem('user') || 'null')
            if (token && user && String(user.id || '')) this.data = { token, user }
            else this.clear()
        } catch { try { this.clear() } catch { /* In-memory state remains signed out. */ } }
    }
    begin() { this.clear(); return this.revision }
    current(revision: number) { return revision === this.revision }
    reload() {
        const restored = new AuthSession(this.storage)
        ++this.revision
        this.data = restored.data
    }
    commit(revision: number, token: string, user: SessionUser) {
        if (!this.current(revision)) return false
        if (!token || !user || !String(user.id || '')) throw new Error('登录响应不完整')
        try {
            this.storage.setItem('token', token)
            this.storage.setItem('user', JSON.stringify(user))
        } catch (error) { this.clear(); throw error }
        this.data = { token, user }
        return true
    }
    clear() {
        ++this.revision
        this.data = { token: '', user: null }
        // Always attempt both removals, even when persistence is unavailable.
        try { this.storage.removeItem('token') } finally { this.storage.removeItem('user') }
    }
    unauthorized(revision: number, authorization: string) {
        if (!this.data.token || !this.current(revision) || authorization !== `Bearer ${this.data.token}`) return false
        this.clear()
        return true
    }
}
