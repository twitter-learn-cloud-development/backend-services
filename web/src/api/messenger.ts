import request from '../utils/request'
import type { User } from './tweet'

export interface Message {
    id: string
    sender_id: string
    receiver_id: string
    content: string
    created_at: number
    is_read: boolean
    sender?: User
}

export interface Conversation {
    peer_id: string
    latest_message: Message
    unread_count: number
    peer?: User
}

export interface SendMessageRequest {
    receiver_id: string
    content: string
}

export const getConversations = (params: { limit?: number; cursor?: string }) => {
    return request({
        url: '/messenger/conversations',
        method: 'get',
        params
    })
}

export const getMessages = (peerId: string, params: { limit?: number; cursor?: string }) => {
    return request({
        url: `/messenger/conversations/${peerId}/messages`,
        method: 'get',
        params
    })
}

export const sendMessage = (data: SendMessageRequest) => {
    return request({
        url: '/messenger/messages',
        method: 'post',
        data
    })
}
