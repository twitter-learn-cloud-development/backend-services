import request from '../utils/request'

export const uploadMedia = (file: File) => {
    const formData = new FormData()
    formData.append('file', file)

    return request({
        url: '/upload',
        method: 'post',
        data: formData,
        headers: {
            'Content-Type': 'multipart/form-data'
        }
    })
}
