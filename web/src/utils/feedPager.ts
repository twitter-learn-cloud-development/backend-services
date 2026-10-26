export interface FeedPage<T> { tweets: T[]; next_cursor: string; has_more: boolean }
export interface FeedState<T> { items: T[]; cursor: string; hasMore: boolean; loading: boolean; error: string | null; continuation: boolean }

// Server cursors stay opaque strings. A generation isolates tab/account/refresh
// requests; at most three no-new-item pages are drained per user action.
export class FeedPager<T extends { id: string }> {
    state: FeedState<T> = { items: [], cursor: '0', hasMore: true, loading: false, error: null, continuation: false }
    private generation = 0
    private disposed = false
    private retryReplace = false
    private fetchPage: (cursor: string) => Promise<FeedPage<T>>
    private publish: (state: FeedState<T>) => void

    constructor(fetchPage: (cursor: string) => Promise<FeedPage<T>>, publish: (state: FeedState<T>) => void) {
        this.fetchPage = fetchPage
        this.publish = publish
    }

    private set(patch: Partial<FeedState<T>>) { this.state = { ...this.state, ...patch }; this.publish(this.state) }
    updateItems(update: (items: T[]) => T[]) { this.set({ items: update(this.state.items) }) }
    dispose() { this.disposed = true; this.generation++ }
    async reset(fetchPage: (cursor: string) => Promise<FeedPage<T>>) {
        this.fetchPage = fetchPage
        this.generation++
        this.retryReplace = false
        this.set({ items: [], cursor: '0', hasMore: true, loading: false, error: null, continuation: false })
        await this.load()
    }
    async refresh() {
        this.generation++
        this.retryReplace = true
        this.set({ cursor: '0', hasMore: true, loading: false, error: null, continuation: false })
        await this.load(true)
    }
    async load(replace = this.retryReplace) {
        if (this.disposed || this.state.loading || !this.state.hasMore) return
        const generation = this.generation
        const fetchPage = this.fetchPage
        this.set({ loading: true, error: null, continuation: false })
        let replacing = replace
        try {
            for (let attempt = 0; attempt < 3; attempt++) {
                const requested = this.state.cursor
                const page = await fetchPage(requested)
                if (this.disposed || generation !== this.generation) return
                if (!Array.isArray(page.tweets) || typeof page.next_cursor !== 'string' || typeof page.has_more !== 'boolean' ||
                    (page.has_more && (!page.next_cursor || page.next_cursor === '0' || page.next_cursor === requested))) {
                    throw new Error('分页结果无效，请刷新重试')
                }
                const previous = replacing ? [] : this.state.items
                const ids = new Set(previous.map(item => item.id))
                const added: T[] = []
                for (const item of page.tweets) {
                    if (!item || typeof item.id !== 'string' || !item.id) throw new Error('推文数据无效，请刷新重试')
                    if (!ids.has(item.id)) { ids.add(item.id); added.push(item) }
                }
                this.set({ items: [...previous, ...added], cursor: page.next_cursor, hasMore: page.has_more })
                this.retryReplace = false
                replacing = false
                if (added.length || !page.has_more) return
            }
            this.set({ continuation: true })
        } catch {
            if (!this.disposed && generation === this.generation) this.set({ error: '加载失败，请重试' })
        } finally {
            if (!this.disposed && generation === this.generation) this.set({ loading: false })
        }
    }
}
