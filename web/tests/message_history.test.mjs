import test from 'node:test'
import assert from 'node:assert/strict'
import { MessageHistory } from '../src/utils/messageHistory.ts'
const msg = (id, read = false) => ({ id, sender_id: '2', receiver_id: '1', is_read: read })
const page = (ids, next = '', more = false) => ({ messages: ids.map(id => msg(id, true)), next_cursor: next, has_more: more })
const make = fetch => new MessageHistory(fetch, m => m.sender_id === '2' && m.receiver_id === '1', () => {})
test('history appends exact large IDs and deduplicates HTTP / Socket without regressing read', async () => {
    const pages = [page(['9007199254740999', '9007199254740998'], '9007199254740998', true), page(['9007199254740997'])]
    const h = make(async () => pages.shift()); await h.older()
    h.push(msg('9007199254740999')); await h.older()
    assert.deepEqual(h.items.map(m => m.id), ['9007199254740997', '9007199254740998', '9007199254740999'])
    assert.equal(h.items.at(-1).is_read, true); assert.equal(h.hasMore, false)
})
test('reconnect recovery spans more than a page without moving anchor for socket hints', async () => {
    const requests = [], pages = [page(['10', '9'], '9', true), page(['20', '19'], '19', true), page(['18', '17'], '17', true), page(['16', '15'], '15', true), page(['14', '13'], '13', true), page(['12', '11'], '11', true), page(['10', '9'], '9', true), page(['8'])]
    const h = make(async cursor => { requests.push(cursor); return pages.shift() }); await h.older()
    h.push(msg('21')); await h.recover()
    assert.equal(h.continuation, true); assert.equal(h.cursor, '9'); assert.equal(requests.length, 4)
    await h.recover(); assert.equal(h.continuation, false); assert.equal(requests.at(-1), '11')
    await h.older(); assert.equal(requests.at(-1), '9'); assert.equal(h.items.at(-1).id, '21')
})
test('failed recovery resumes its cursor and never advances past failed page', async () => {
    let calls = 0; const requested = []
    const h = make(async cursor => { requested.push(cursor); calls++; if (calls === 1) return page(['5']); if (calls === 2) return page(['9', '8'], '8', true); if (calls === 3) throw Error('offline'); return page(['7', '6', '5']) })
    await h.older(); await h.recover(); assert.ok(h.error)
    await h.recover(); assert.deepEqual(requested, ['0', '0', '8', '8']); assert.equal(h.continuation, false)
})
test('switch/dispose discards late success and failure; duplicate fetch is bounded', async () => {
    let resolve, calls = 0
    const h = make(() => { calls++; return new Promise(done => { resolve = done }) })
    const pending = h.older(); await h.older(); await h.recover(); assert.equal(calls, 1)
    h.dispose(); resolve(page(['5'])); await pending; assert.deepEqual(h.items, [])
})
test('invalid party/order/nonadvancing cursor does not corrupt history', async () => {
    for (const value of [page(['5', '6']), page(['5'], '4', true), page([], '5', true), page(['18446744073709551616']), page([5]), { messages: [{ ...msg('5'), receiver_id: '99' }], next_cursor: '', has_more: false }]) {
        const h = make(async () => value); await h.older()
        assert.ok(h.error); assert.deepEqual(h.items, [])
    }
})
