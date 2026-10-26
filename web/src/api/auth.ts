import request from '../utils/request'

export const changePassword = (data: { old_password: string; new_password: string }) => request({ url: '/auth/password', method: 'post', data })
export const logout = () => request({ url: '/auth/logout', method: 'post', data: { scope: 'local' } })

export const login = (data: any) => {
    return request({
        url: '/auth/login',
        method: 'post',
        data
    })
}

export const register = (data: any) => {
    return request({
        url: '/auth/register',
        method: 'post',
        data
    })
}

export const getMe = () => {
    return request({
        url: '/users/me',
        method: 'get'
    })
}
