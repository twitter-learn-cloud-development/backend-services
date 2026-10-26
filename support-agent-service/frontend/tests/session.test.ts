import assert from 'node:assert/strict';
import { afterEach, describe, it } from 'node:test';
import { useWorkspace } from '../src/useWorkspace.ts';

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });
const alpha = { id: 'alpha', username: 'Alpha', email: 'alpha@example.test' };
const beta = { id: 'beta', username: 'Beta', email: 'beta@example.test' };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
function answer(text: string) {
  return Response.json({
    request_id: '11111111-1111-4111-8111-111111111111',
    conversation_id: '22222222-2222-4222-8222-222222222222',
    case_id: '33333333-3333-4333-8333-333333333333',
    status: 'answered', answer: text, suggested_actions: [], evidence_refs: [], citations: [],
    created_at: '2026-10-02T00:00:00Z', trace_id: 'fixture',
  });
}

describe('account generation isolation', () => {
  it('initializes an unauthenticated real widget without demo conversations', () => {
    const workspace = useWorkspace();
    workspace.connectSession('', null);
    assert.equal(workspace.mode.value, 'live');
    assert.equal(workspace.threads.value.length, 0);
  });

  it('ignores a late old response without clearing the newer request busy state', async () => {
    const first = deferred<Response>();
    const second = deferred<Response>();
    let calls = 0;
    // Deliberately ignore abort, to prove revision isolation beyond transport cancellation.
    globalThis.fetch = async () => (++calls === 1 ? first.promise : second.promise);
    const workspace = useWorkspace();
    workspace.connectSession('token-alpha', alpha);
    workspace.draft.value = 'Alpha question';
    const firstSend = workspace.send();
    workspace.connectSession('token-beta', beta);
    assert.equal(workspace.messages.value.length, 0);
    workspace.draft.value = 'Beta question';
    const secondSend = workspace.send();
    first.resolve(answer('Late Alpha answer'));
    await firstSend;
    assert.equal(workspace.busy.value, true);
    assert.deepEqual(workspace.messages.value.map(message => message.text), ['Beta question']);
    second.resolve(answer('Beta answer'));
    await secondSend;
    assert.equal(workspace.busy.value, false);
    assert.deepEqual(workspace.messages.value.map(message => message.text), ['Beta question', 'Beta answer']);
  });

  it('does not let a late sign-in overwrite the newer host identity', async () => {
    const pendingLogin = deferred<Response>();
    globalThis.fetch = async (url) => String(url).endsWith('/auth/login')
      ? pendingLogin.promise : Response.json({ user: alpha });
    const workspace = useWorkspace();
    const login = workspace.signIn(alpha.email, 'fixture-password');
    workspace.connectSession('token-beta', beta);
    pendingLogin.resolve(Response.json({ token: 'late-token-alpha' }));
    await login;
    assert.equal(workspace.token.value, 'token-beta');
    assert.equal(workspace.account.value?.id, 'beta');
    assert.equal(workspace.loginBusy.value, false);
    assert.equal(workspace.loginError.value, '');
  });
});
