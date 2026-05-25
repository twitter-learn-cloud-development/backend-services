import request from '../utils/request'

// 获取所有可用的大模型以及它们支持的文件类型
export const getModels = () => {
    return request({
        url: '/agent/models',
        method: 'get'
    })
}

// 获取历史对话会话列表
export const getDialogues = () => {
    return request({
        url: '/agent/dialogues',
        method: 'get'
    })
}

// 获取某个特定对话的详细消息上下文
export const getDialogueMessages = (dialogueId: string) => {
    return request({
        url: `/agent/dialogues/${dialogueId}/messages`,
        method: 'get'
    })
}

// 模式一：普通直接对话
export const chat = (data: { content: string, dialogue_id: number | string, model_kind_id: number | string }) => {
    return request({
        url: '/agent/chat',
        method: 'post',
        data,
        timeout: 60000
    })
}

// 模式二：推文推荐与资讯咨询
export const consult = (data: { content: string, dialogue_id: number | string, model_kind_id: number | string }) => {
    return request({
        url: '/agent/consult',
        method: 'post',
        data,
        timeout: 60000
    })
}

// 模式三：AI 辅助发推 (生成草稿候选)
export const assistPublish = (data: { content: string, dialogue_id: number | string, model_kind_id: number | string }) => {
    return request({
        url: '/agent/assist',
        method: 'post',
        data,
        timeout: 60000
    })
}

// 模式三确认发布
export const confirmPublish = (data: { content: string }) => {
    return request({
        url: '/agent/confirm',
        method: 'post',
        data
    })
}

// 模式四：多智能体协作自动生成推文
export const multiAgentPublish = (data: { 
    domain: string, 
    author_user_id: string, 
    style_ratio: number, 
    reference_tweet_ids: string[],
    content: string 
}) => {
    return request({
        url: '/agent/multi',
        method: 'post',
        data,
        timeout: 120000 // 多体协作可能更耗时
    })
}

// 上传文件并由 Agent 解析（作为系统提示进入对话上下文）
export const uploadFileAnalysis = (file: File, fileKindId: string) => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('file_kind_id', fileKindId)
    
    return request({
        url: '/agent/files/analysis',
        method: 'post',
        data: formData,
        headers: {
            'Content-Type': 'multipart/form-data'
        },
        timeout: 60000
    })
}
