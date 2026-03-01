import { defineStore } from 'pinia'
import { ref } from 'vue'
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

    // 简单的事件总线，用于组件监听新消息
    const messageHandlers = ref<Array<(msg: Message) => void>>([])

    const connect = () => {
        if (socket.value?.readyState === WebSocket.OPEN) return
        if (!userStore.token) return

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const host = window.location.host
        // 注意：/api/v1/ws 是 Gateway 的路由
        const wsUrl = `${protocol}//${host}/api/v1/ws?token=${userStore.token}`

        try {
            const ws = new WebSocket(wsUrl)
            socket.value = ws

            ws.onopen = () => {
                console.log('✅ WebSocket Connected')
                isConnected.value = true
            }

            ws.onmessage = (event) => {
                try {
                    const payload = JSON.parse(event.data)
                    // 后端推送的消息结构: { type: "message", data: { ... } }
                    if (payload && payload.type === 'message') {
                        const msg = payload.data as Message
                        // 触发监听器
                        messageHandlers.value.forEach(handler => handler(msg))
                        console.log('📩 New Message:', msg)
                    }
                } catch (e) {
                    console.error('WebSocket message parse error:', e)
                }
            }

            ws.onclose = () => {
                console.log('❌ WebSocket Disconnected')
                isConnected.value = false
                socket.value = null
                // TODO: 简单的重连机制 (后端 WS 尚未实现，暂时屏蔽以避免 Vite Proxy 报错刷屏)
                setTimeout(() => {
                    if (userStore.token) connect()
                }, 5000)
            }
        } catch (e) {
            console.error('WebSocket connection error:', e)
        }
    }

    const disconnect = () => {
        if (socket.value) {
            socket.value.close()
            socket.value = null
            isConnected.value = false
        }
    }

    const onMessage = (handler: (msg: Message) => void) => {
        messageHandlers.value.push(handler)
    }

    const offMessage = (handler: (msg: Message) => void) => {
        const index = messageHandlers.value.indexOf(handler)
        if (index > -1) {
            messageHandlers.value.splice(index, 1)
        }
    }

    return {
        isConnected,
        connect,
        disconnect,
        onMessage,
        offMessage
    }
})
