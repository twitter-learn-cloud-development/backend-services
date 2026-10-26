import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { hasSiteAiHandoff, SITE_AI_HANDOFF_ACTION, siteAiHref } from '../src/handoff.ts'

describe('guide handoff boundary', () => {
  it('shows handoff only for the exact runtime-owned action', () => {
    assert.equal(hasSiteAiHandoff(undefined), false)
    assert.equal(hasSiteAiHandoff({ suggested_actions: [] }), false)
    assert.equal(hasSiteAiHandoff({ suggested_actions: ['refresh'] }), false)
    assert.equal(hasSiteAiHandoff({ suggested_actions: ['javascript:alert(1)'] }), false)
    assert.equal(hasSiteAiHandoff({ suggested_actions: [SITE_AI_HANDOFF_ACTION] }), true)
  })

  it('builds a fixed same-site route without question or credential data', () => {
    assert.equal(
      siteAiHref('https://twitter.example/base?token=secret#fragment'),
      'https://twitter.example/agent',
    )
  })

  it('rejects unsafe configured URLs and never accepts an action URL as navigation', () => {
    assert.throws(() => siteAiHref('javascript:alert(1)'))
    assert.throws(() => siteAiHref('https://user:password@twitter.example'))
    assert.throws(() => siteAiHref('data:text/html,<script>'))
    assert.equal(hasSiteAiHandoff({ suggested_actions: ['https://evil.example/agent'] }), false)
  })
})
