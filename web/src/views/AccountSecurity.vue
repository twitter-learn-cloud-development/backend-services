<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import MainLayout from '../layout/MainLayout.vue'
import { changePassword } from '../api/auth'
import { useUserStore } from '../stores/user'

const oldPassword = ref(''), newPassword = ref(''), confirmation = ref(''), error = ref(''), busy = ref(false)
const user = useUserStore(), router = useRouter()
async function submit() {
  if (busy.value) return
  const length = new TextEncoder().encode(newPassword.value).length
  if (length < 6 || length > 72 || newPassword.value !== confirmation.value) { error.value = '新密码须为 6–72 UTF-8 字节，两次输入须一致'; return }
  const epoch = user.revision
  busy.value = true; error.value = ''
  try {
    await changePassword({ old_password: oldPassword.value, new_password: newPassword.value })
    if (user.isCurrent(epoch)) { user.logout(); void router.replace('/login') }
  } catch (e: any) {
    if (user.isCurrent(epoch)) error.value = e.response?.data?.error || '请求失败，结果可能未确定。请尝试用新密码登录；不要自动重试。'
  } finally { oldPassword.value = ''; newPassword.value = ''; confirmation.value = ''; busy.value = false }
}
</script>

<template>
  <MainLayout>
    <section class="p-6 max-w-xl space-y-5">
      <h1 class="text-xl font-bold">账号安全</h1>
      <p>修改密码后，本浏览器需要重新登录。其他设备和已签发令牌仍有效至到期，当前暂不支持全设备撤销。</p>
      <form v-if="user.token" class="space-y-4" @submit.prevent="submit">
        <label class="block">当前密码<input v-model="oldPassword" type="password" autocomplete="current-password" required class="block border rounded p-2 w-full" /></label>
        <label class="block">新密码<input v-model="newPassword" type="password" autocomplete="new-password" required class="block border rounded p-2 w-full" /></label>
        <label class="block">确认新密码<input v-model="confirmation" type="password" autocomplete="new-password" required class="block border rounded p-2 w-full" /></label>
        <p v-if="error" role="alert">{{ error }}</p>
        <button :disabled="busy" class="bg-primary text-white rounded px-4 py-2">{{ busy ? '提交中…' : '修改密码并重新登录' }}</button>
      </form>
      <RouterLink v-else to="/login">请先登录</RouterLink>
    </section>
  </MainLayout>
</template>
