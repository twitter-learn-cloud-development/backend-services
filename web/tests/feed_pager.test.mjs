import test from 'node:test'
import assert from 'node:assert/strict'
import { FeedPager } from '../src/utils/feedPager.ts'

const page = (ids, cursor = '0', more = false) => ({ tweets: ids.map(id => ({ id })), next_cursor: cursor, has_more: more })
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }

test('multiple pages deduplicate and preserve large opaque cursors', async () => {
    const cursors = []
    const pages = [page(['9007199254740999', '9007199254740998'], '9007199254740998', true), page(['9007199254740998', '9007199254740997'])]
    const pager = new FeedPager(async cursor => { cursors.push(cursor); return pages.shift() }, () => {})
    await pager.refresh(); await pager.load()
    assert.deepEqual(cursors, ['0', '9007199254740998'])
    assert.deepEqual(pager.state.items.map(row => row.id), ['9007199254740999', '9007199254740998', '9007199254740997'])
    assert.equal(pager.state.hasMore, false)
})

test('empty pages are bounded, resumable and never treated as terminal', async () => {
    let calls = 0
    const pager = new FeedPager(async () => page([], String(100 - calls++), true), () => {})
    await pager.refresh()
    assert.equal(calls, 3); assert.equal(pager.state.continuation, true); assert.equal(pager.state.cursor, '98')
    await pager.load(); assert.equal(calls, 6); assert.equal(pager.state.cursor, '95')
})

test('tab replacement isolates late response and old finally state', async () => {
    const old = deferred(), fresh = deferred()
    const pager = new FeedPager(() => old.promise, () => {})
    const first = pager.load()
    const next = pager.reset(() => fresh.promise)
    old.resolve(page(['old'])); await first
    assert.equal(pager.state.loading, true); assert.deepEqual(pager.state.items, [])
    fresh.resolve(page(['fresh'])); await next
    assert.deepEqual(pager.state.items.map(row => row.id), ['fresh'])
})

test('refresh supersedes load-more; duplicate actions share one active request', async () => {
    const pending = deferred()
    let calls = 0
    const pager = new FeedPager(async () => { calls++; return calls === 1 ? page(['5'], '5', true) : calls === 2 ? pending.promise : page(['9']) }, () => {})
    await pager.load()
    const loading = pager.load(); await pager.load(); assert.equal(calls, 2)
    await pager.refresh(); pending.resolve(page(['4'])); await loading
    assert.deepEqual(pager.state.items.map(row => row.id), ['9'])
})

test('error preserves cursor/items and retry does not skip the failed page', async () => {
    let calls = 0; const requested = []
    const pager = new FeedPager(async cursor => { requested.push(cursor); if (++calls === 2) throw new Error('SQL details'); return calls === 1 ? page(['5'], '5', true) : page(['4']) }, () => {})
    await pager.load(); await pager.load()
    assert.equal(pager.state.cursor, '5'); assert.equal(pager.state.error, '加载失败，请重试')
    await pager.load(); assert.deepEqual(requested, ['0', '5', '5'])
    assert.deepEqual(pager.state.items.map(row => row.id), ['5', '4'])
})

test('non-advancing cursor fails without endless requests; dispose drops response', async () => {
    let calls = 0
    const pager = new FeedPager(async () => { calls++; return page([], '0', true) }, () => {})
    await pager.load(); assert.equal(calls, 1); assert.ok(pager.state.error)
    const pending = deferred(); const other = new FeedPager(() => pending.promise, () => {})
    const request = other.load(); other.dispose(); pending.resolve(page(['5'])); await request
    assert.deepEqual(other.state.items, [])
})

test('failed refresh retry replaces old list after a successful fresh page', async () => {
    let calls = 0
    const pager = new FeedPager(async () => { if (++calls === 2) throw new Error('offline'); return calls === 1 ? page(['old'], '5', true) : page(['new']) }, () => {})
    await pager.load(); await pager.refresh()
    assert.deepEqual(pager.state.items.map(row => row.id), ['old'])
    await pager.load()
    assert.deepEqual(pager.state.items.map(row => row.id), ['new'])
})
