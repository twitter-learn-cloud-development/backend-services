import type { SupportResponse } from './types.ts'

export const SITE_AI_HANDOFF_ACTION = 'open_site_ai_assistant'

export function hasSiteAiHandoff(response?: Pick<SupportResponse, 'suggested_actions'>): boolean {
  return response?.suggested_actions.includes(SITE_AI_HANDOFF_ACTION) === true
}

// Only a configured trusted HTTP(S) origin + a fixed route. No model URL or query data.
export function siteAiHref(configuredBase = import.meta.env?.VITE_TWITTER_WEB_URL || 'http://127.0.0.1:5173'): string {
  const base = new URL(configuredBase)
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password) {
    throw new Error('站内 AI 助手地址未正确配置。')
  }
  return new URL('/agent', base.origin).href
}
