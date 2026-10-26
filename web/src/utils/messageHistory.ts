export interface HistoryMessage { id: string; sender_id: string; receiver_id: string; is_read: boolean }
export interface MessagePage<T> { messages: T[]; next_cursor: string; has_more: boolean }
export const compareIDs = (a: string, b: string) => a.length - b.length || a.localeCompare(b)
const validID = (id: unknown): id is string => typeof id === 'string' && /^[1-9][0-9]{0,19}$/.test(id) && compareIDs(id, '18446744073709551615') <= 0

// History and recovery have independent cursors. Socket hints never move anchor.
export class MessageHistory<T extends HistoryMessage> {
    items: T[] = []
    cursor = '0'
    hasMore = true
    busy = false
    syncing = false
    continuation = false
    error = ''
    private epoch = 0
    private disposed = false
    private anchor = '0'
    private syncCursor = '0'
    private syncHead = '0'
    private fetchPage: (cursor: string) => Promise<MessagePage<T>>
    private notify: () => void
    private accepts: (item: T) => boolean
    constructor(fetchPage: (cursor: string) => Promise<MessagePage<T>>, accepts: (item: T) => boolean, notify: () => void) {
        this.fetchPage = fetchPage; this.accepts = accepts; this.notify = notify
    }
    private merge(rows: T[]) {
        const map = new Map(this.items.map(item => [item.id, item]))
        for (const item of rows) {
            if (!validID(item.id) || !this.accepts(item)) throw new Error('消息页身份不匹配')
            const old = map.get(item.id)
            map.set(item.id, { ...old, ...item, is_read: Boolean(old?.is_read || item.is_read) })
        }
        this.items = [...map.values()].sort((a, b) => compareIDs(a.id, b.id))
    }
    push(item: T) { if (!this.disposed && this.accepts(item)) { this.merge([item]); this.notify() } }
    private validate(page: MessagePage<T>, cursor: string) {
        if (!Array.isArray(page.messages) || page.messages.length > 100 || typeof page.has_more !== 'boolean' || typeof page.next_cursor !== 'string') throw new Error('消息页格式错误')
        let previous = cursor
        for (const item of page.messages) {
            if (!this.accepts(item) || !validID(item.id) || (previous !== '0' && compareIDs(item.id, previous) >= 0)) throw new Error('消息页顺序错误')
            previous = item.id
        }
        if (page.has_more && (!page.messages.length || page.next_cursor !== previous)) throw new Error('消息游标未推进')
    }
    async older() {
        if (this.disposed || this.busy || this.syncing || !this.hasMore) return
        this.busy = true; this.error = ''; this.notify()
        const epoch = this.epoch, cursor = this.cursor
        try {
            const page = await this.fetchPage(cursor)
            if (this.disposed || epoch !== this.epoch) return
            this.validate(page, cursor); this.merge(page.messages)
            this.cursor = page.next_cursor || '0'; this.hasMore = page.has_more
            if (cursor === '0') this.anchor = page.messages[0]?.id || '0'
        } catch { if (!this.disposed && epoch === this.epoch) this.error = '历史加载失败，请重试' }
        finally { if (!this.disposed && epoch === this.epoch) { this.busy = false; this.notify() } }
    }
    async recover() {
        if (this.disposed || this.busy || this.syncing) return
        if (this.cursor === '0' && !this.items.length && this.hasMore) { await this.older(); return }
        this.syncing = true; this.error = ''; this.notify()
        const epoch = this.epoch
        try {
            for (let i = 0; i < 3; i++) {
                const cursor = this.syncCursor, page = await this.fetchPage(cursor)
                if (this.disposed || epoch !== this.epoch) return
                this.validate(page, cursor)
                if (cursor === '0') this.syncHead = page.messages[0]?.id || this.anchor
                this.merge(page.messages)
                const reached = this.anchor !== '0' && page.messages.some(item => compareIDs(item.id, this.anchor) <= 0)
                if (reached || !page.has_more) {
                    this.anchor = this.syncHead; this.syncCursor = '0'; this.continuation = false; return
                }
                this.syncCursor = page.next_cursor; this.continuation = true
            }
        } catch { if (!this.disposed && epoch === this.epoch) this.error = '消息同步失败，继续同步可重试' }
        finally { if (!this.disposed && epoch === this.epoch) { this.syncing = false; this.notify() } }
    }
    dispose() { this.disposed = true; ++this.epoch }
}
