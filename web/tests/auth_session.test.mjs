import test from 'node:test'
import assert from 'node:assert/strict'
import { AuthSession } from '../src/utils/authSession.ts'

const storage = (values = {}) => { const data = new Map(Object.entries(values)); return { getItem: key => data.get(key) ?? null, setItem: (key, value) => data.set(key, value), removeItem: key => data.delete(key) } }
const user = { id: '9007199254740999', username: 'alice' }
test('broken or incomplete persisted session fails closed', () => {
    for (const values of [{ token: 'old', user: '{broken' }, { token: 'old' }, { user: JSON.stringify(user) }]) {
        const saved = storage(values), session = new AuthSession(saved)
        assert.equal(session.data.token, ''); assert.equal(saved.getItem('token'), null); assert.equal(saved.getItem('user'), null)
    }
})
test('logout prevents late login restoring a session; latest attempt wins', () => {
    const session = new AuthSession(storage()), old = session.begin()
    session.clear(); assert.equal(session.commit(old, 'old', user), false)
    const earlier = session.begin(), latest = session.begin()
    assert.equal(session.commit(latest, 'new', user), true)
    assert.equal(session.commit(earlier, 'old', user), false)
    assert.equal(session.data.token, 'new')
})
test('old 401 cannot clear new session even if JWT string is identical', () => {
    const session = new AuthSession(storage()), old = session.begin()
    session.commit(old, 'same-jwt', user)
    const current = session.begin(); session.commit(current, 'same-jwt', user)
    assert.equal(session.unauthorized(old, 'Bearer same-jwt'), false)
    assert.equal(session.unauthorized(current, ''), false)
    assert.equal(session.unauthorized(current, 'Bearer same-jwt'), true)
    assert.equal(session.data.user, null)
})
test('login response persists both fields; sign-out preserves preferences', () => {
    const saved = storage({ theme: 'dark' }), session = new AuthSession(saved)
    session.commit(session.begin(), 'jwt', user)
    const restored = new AuthSession(saved)
    assert.equal(restored.data.user.id, user.id)
    restored.clear(); assert.equal(saved.getItem('theme'), 'dark'); assert.equal(saved.getItem('token'), null)
})
test('failed second persistence write clears partial token', () => {
    const saved = storage(), session = new AuthSession(saved), set = saved.setItem
    saved.setItem = (key, value) => { if (key === 'user') throw new Error('quota'); set(key, value) }
    assert.throws(() => session.commit(session.begin(), 'jwt', user), /quota/)
    assert.equal(session.data.token, ''); assert.equal(saved.getItem('token'), null)
})
test('cross-tab identity reload adopts complete session and fences pending work', () => {
    const saved = storage(), first = new AuthSession(saved), second = new AuthSession(saved)
    const pending = second.begin()
    first.commit(first.begin(), 'fresh', user)
    second.reload(); assert.equal(second.data.token, 'fresh'); assert.equal(second.commit(pending, 'late', user), false)
    first.clear(); second.reload(); assert.equal(second.data.token, '')
})
