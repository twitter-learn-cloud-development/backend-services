import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { useUserStore } from './user'

export interface Message {
    id: string
    sender_id: string
    receiver_id: string
    content: string
    created_at: number
    is_read: boolean
}

export const useMessengerStore = defineStore('messenger', () => {
    const userStore = useUserStore()
    const socket = ref<WebSocket | null>(null)
    const isConnected = ref(false)

    const messageHandlers = new Set<(msg: Message) => void>()
    const readyHandlers = new Set<() => void>()
    const ready = () => readyHandlers.forEach(handler => handler())
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let shouldReconnect = false

    const connect = () => {
        if (!userStore.token) return
        shouldReconnect = true
        if (reconnectTimer) {
            clearTimeout(reconnectTimer)
            reconnectTimer = null
        }
        if (socket.value && (socket.value.readyState === WebSocket.OPEN || socket.value.readyState === WebSocket.CONNECTING)) return

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const host = window.location.host
        // 注意：/api/v1/ws 是 Gateway 的路由
        const wsUrl = `${protocol}//${host}/api/v1/ws?token=${userStore.token}`

        try {
            const ws = new WebSocket(wsUrl)
            socket.value = ws

            ws.onopen = () => {
                if (socket.value !== ws) return
                isConnected.value = true
                ready()
            }

            ws.onmessage = (event) => {
                if (socket.value !== ws) return
                try {
                    const payload = JSON.parse(event.data)
                    if (payload?.type === 'ready') { ready(); return }
                    // 后端推送的消息结构: { type: "message", data: { ... } }
                    if (payload && payload.type === 'message') {
                        const msg = payload.data as Message
                        // 触发监听器
                        messageHandlers.forEach(handler => handler(msg))
                    }
                } catch { /* Untrusted hint; HTTP history remains authoritative. */ }
            }

            ws.onclose = () => {
                if (socket.value !== ws) return
                isConnected.value = false
                socket.value = null
                if (shouldReconnect && userStore.token) {
                    reconnectTimer = setTimeout(connect, 5000)
                }
            }
        } catch {
            if (shouldReconnect) reconnectTimer = setTimeout(connect, 5000)
        }
    }

    const disconnect = () => {
        shouldReconnect = false
        if (reconnectTimer) {
            clearTimeout(reconnectTimer)
            reconnectTimer = null
        }
        const ws = socket.value
        socket.value = null
        isConnected.value = false
        ws?.close()
    }

    const onMessage = (handler: (msg: Message) => void) => {
        messageHandlers.add(handler)
    }

    const offMessage = (handler: (msg: Message) => void) => {
        messageHandlers.delete(handler)
    }

    watch(() => userStore.token, () => { disconnect(); if (userStore.token) connect() }, { flush: 'sync' })

    return {
        isConnected,
        connect,
        disconnect,
        onMessage,
        offMessage,
        onReady: (handler: () => void) => { readyHandlers.add(handler) },
        offReady: (handler: () => void) => { readyHandlers.delete(handler) }
    }
})
