// Offline Playwright fixture checks for the embeddable support widget.
// All auth, readiness, and chat requests are intercepted; no account/model API is called.
const assert = require("node:assert/strict");
const fs = require('node:fs/promises');
const path = require("node:path");
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE_DIR
    ? path.join(process.env.PLAYWRIGHT_MODULE_DIR, "playwright")
    : "playwright",
);

const HOST = process.env.SUPPORT_WIDGET_HOST || "http://127.0.0.1:5174/";
const IDs = {
  request: "11111111-1111-4111-8111-111111111111",
  conversation: "22222222-2222-4222-8222-222222222222",
  case: "33333333-3333-4333-8333-333333333333",
};

function response(message, account, status = "answered") {
  const citation = {
    id: "tweet:987654321012345678",
    kind: "tweet",
    label: `fixture citation for ${account}`,
    path: "/tweets/987654321012345678",
    evidence_ref: "tweet:987654321012345678",
  };
  return {
    request_id: IDs.request,
    conversation_id: IDs.conversation,
    case_id: IDs.case,
    status,
    answer: `fixture answer for ${account}: ${message}`,
    suggested_actions: [],
    evidence_refs: [citation.evidence_ref],
    citations: [citation],
    created_at: "2026-10-02T01:02:03Z",
    trace_id: "fixture-trace",
  };
}

async function main() {
  const browser = await chromium.launch({
    channel: process.env.BROWSER_CHANNEL || "msedge",
    headless: true,
  });
  try {
    const context = await browser.newContext({
      viewport: { width: 1280, height: 850 },
      locale: "zh-CN",
      timezoneId: "Asia/Shanghai",
    });
    await context.addInitScript(() => {
      window.__openedUrls = [];
      window.open = (url) => {
        window.__openedUrls.push(String(url));
        return null;
      };
    });
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    const pageErrors = [];
    const chatRequests = [];
    let oldRequestStarted;
    const oldRequest = new Promise((resolve) => {
      oldRequestStarted = resolve;
    });
    page.on("pageerror", (error) => pageErrors.push(error.message));

    // Register the catch-all first: Playwright gives later routes precedence.
    await page.route("**/api/**", async (route) => {
      await route.fulfill({ status: 599, json: { error: "unmocked fixture request" } });
    });
    await page.route("**/health/ready", (route) =>
      route.fulfill({ json: { status: "ready" } }),
    );
    await page.route("**/api/v1/auth/login", async (route) => {
      const payload = route.request().postDataJSON();
      const suffix = payload.email.startsWith("alpha") ? "alpha" : "beta";
      await route.fulfill({ json: { token: `fixture-token-${suffix}` } });
    });
    await page.route("**/api/v1/users/me", async (route) => {
      const suffix = route.request().headers().authorization.endsWith("alpha")
        ? "alpha"
        : "beta";
      await route.fulfill({
        json: {
          user: {
            id: `fixture-${suffix}`,
            username: suffix === "alpha" ? "Alpha" : "Beta",
            email: `${suffix}@example.test`,
          },
        },
      });
    });
    await page.route("**/api/v1/support/chat", async (route) => {
      const request = {
        headers: route.request().headers(),
        authorization: route.request().headers().authorization,
        body: route.request().postDataJSON(),
      };
      chatRequests.push(request);
      const who = request.authorization.endsWith("alpha") ? "Alpha" : "Beta";
      if (request.body.message === "迟到的 Alpha 请求") {
        oldRequestStarted();
        await new Promise((resolve) => setTimeout(resolve, 900));
      }
      try {
        const failure = /^fixture-error:(\d+)$/.exec(request.body.message);
        if (failure) {
          await route.fulfill({
            status: Number(failure[1]),
            json: { request_id: IDs.request, code: "fixture_error" },
          });
          return;
        }
        const statusCase = /^fixture-status:(\w+)$/.exec(request.body.message);
        await route.fulfill({
          json: response(request.body.message, who, statusCase?.[1] || "answered"),
        });
      } catch {
        // A correct identity switch aborts this old iframe request.
      }
    });
    await page.goto(HOST);
    await page.getByRole("heading", { name: "有什么想解决的？" }).waitFor();
    const launcher = page.getByRole("button", { name: "打开小栖客服" });
    await launcher.click();
    let frameElement = page.locator('iframe[title="小栖客服对话"]');
    await frameElement.waitFor();
    assert.equal(new URL(await frameElement.getAttribute("src")).searchParams.get("demo"), "1");
    let frame = page.frameLocator('iframe[title="小栖客服对话"]');

    // Minimize hides the panel without unmounting the iframe or discarding draft state.
    await frame.getByLabel("客服问题", { exact: true }).fill("保留在收起状态的草稿");
    await frame.locator("body").evaluate((body) => {
      body.dataset.widgetMarker = "same-document";
    });
    await frame.getByRole("button", { name: "收起客服" }).click();
    await page.getByRole("button", { name: "打开小栖客服" }).waitFor();
    assert.equal(await frameElement.count(), 1);
    await page.getByRole("button", { name: "打开小栖客服" }).click();
    assert.equal(await frame.getByLabel("客服问题", { exact: true }).inputValue(), "保留在收起状态的草稿");
    assert.equal(await frame.locator("body").getAttribute("data-widget-marker"), "same-document");

    // Forged messages with either a wrong origin or wrong source must be ignored.
    await page.evaluate(() => {
      const iframe = document.querySelector('iframe[title="小栖客服对话"]');
      window.dispatchEvent(new MessageEvent("message", {
        data: { type: "support:login" },
        origin: "https://attacker.example",
        source: iframe.contentWindow,
      }));
      window.dispatchEvent(new MessageEvent("message", {
        data: { type: "support:login" },
        origin: location.origin,
        source: window,
      }));
    });
    assert.equal(await page.getByRole("dialog").count(), 0, "untrusted bridge messages must not open login");

    // Chinese IME composition Enter must not submit a live or demo message.
    await frame.getByLabel("客服问题", { exact: true }).fill("IME 测试");
    await frame.getByLabel("客服问题", { exact: true }).evaluate((node) => {
      node.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter", bubbles: true, isComposing: true, keyCode: 229,
      }));
    });
    assert.equal(chatRequests.length, 0);
    assert.equal(await frame.getByLabel("客服问题", { exact: true }).inputValue(), "IME 测试");

    // Exercise real host login with fixture credentials, then ensure the iframe receives only that session.
    await page.getByRole("button", { name: "连接账号", exact: true }).click();
    await page.getByRole("button", { name: /真实连接 查询当前账号/ }).click();
    await page.getByLabel("账号邮箱").fill("alpha@example.test");
    await page.getByLabel("密码", { exact: true }).fill("fixture-password");
    await page.getByRole("button", { name: "登录并开始真实咨询" }).click();
    await page.getByRole("dialog").waitFor({ state: "hidden" });
    try {
      await frame.getByText("已连接 @Alpha", { exact: true }).waitFor();
    } catch (error) {
      console.error(JSON.stringify({ step: 'alpha-session', caption: await frame.locator('.mini-header p').innerText(), src: await frameElement.getAttribute('src'), pageErrors, header: await page.locator('.account-button').allTextContents() }));
      throw error;
    }
    await frame.locator('body').evaluate(body => { body.dataset.widgetMarker = 'same-document' });
    await frame.locator('body').evaluate(() => {
      const data = { type: 'support:session', token: 'forged-token', account: { id: 'forged', username: 'Forged', email: 'forged@example.test' } };
      window.dispatchEvent(new MessageEvent('message', { data, origin: 'https://attacker.example', source: window.parent }));
      window.dispatchEvent(new MessageEvent('message', { data, origin: location.origin, source: window }));
    });
    assert.equal(await frame.locator('.mini-header p').innerText(), '已连接 @Alpha');
    await page.evaluate(() => {
      const iframe = document.querySelector('iframe[title="小栖客服对话"]');
      for (const path of ['https://attacker.example', '//attacker.example', '/tweets/demo-001', '/tweets/1?next=evil']) {
        window.dispatchEvent(new MessageEvent('message', { data: { type: 'support:navigate', path }, origin: location.origin, source: iframe.contentWindow }));
      }
    });
    assert.equal(await page.evaluate(() => window.__openedUrls.length), 0);
    await frame.getByLabel("客服问题", { exact: true }).fill("迟到的 Alpha 请求");
    await frame.getByRole("button", { name: "发送客服问题" }).click();
    await oldRequest;
    assert.equal(chatRequests.at(-1).authorization, "Bearer fixture-token-alpha");

    // Send a trusted parent session update directly, without changing iframe src/reloading it.
    await page.evaluate(() => {
      const iframe = document.querySelector('iframe[title="小栖客服对话"]');
      iframe.contentWindow.postMessage({ type: 'support:session', token: 'fixture-token-beta', account: null }, location.origin);
    });
    await frame.getByRole('button', { name: '登录现有账号后咨询' }).waitFor();
    assert.equal(await frame.locator('.mini-message').count(), 0, 'incomplete new account revokes old identity immediately');
    await page.evaluate(() => {
      const iframe = document.querySelector('iframe[title="小栖客服对话"]');
      iframe.contentWindow.postMessage({
        type: "support:session",
        token: "fixture-token-beta",
        account: { id: "fixture-beta", username: "Beta", email: "beta@example.test" },
      }, location.origin);
    });
    frame = page.frameLocator('iframe[title="小栖客服对话"]');
    await frame.getByText("已连接 @Beta", { exact: true }).waitFor();
    assert.equal(await frame.locator("body").getAttribute("data-widget-marker"), "same-document",
      "switching accounts over the session bridge must preserve the iframe document");
    await page.waitForTimeout(1050);
    assert.equal(await frame.getByText("迟到的 Alpha 请求", { exact: true }).count(), 0,
      "late response/user history from the old account must not enter the new session");
    assert.equal(await frame.getByText('fixture answer for Alpha: 迟到的 Alpha 请求', { exact: true }).count(), 0);

    // A Beta fixture answer has a validated citation; only its internal numeric path is bridged.
    await frame.getByLabel("客服问题", { exact: true }).fill("Beta 查询推文");
    await frame.getByRole("button", { name: "发送客服问题" }).click();
    await frame.getByText("fixture answer for Beta: Beta 查询推文", { exact: true }).waitFor();
    const citation = frame.getByRole("link", { name: "fixture citation for Beta ↗" });
    await citation.click();
    await page.waitForFunction(() => window.__openedUrls.length === 1);
    assert.equal(new URL(await page.evaluate(() => window.__openedUrls[0])).pathname, "/tweets/987654321012345678");
    assert.equal(new URL(await page.evaluate(() => window.__openedUrls[0])).origin, "http://127.0.0.1:5173");

    // Every supported answer status must remain distinguishable in the widget.
    const statusLabels = {
      answered: "已答复",
      partial: "部分结果",
      clarification_required: "需要补充信息",
      unsupported: "当前能力不支持",
    };
    for (const [status, label] of Object.entries(statusLabels)) {
      await frame.getByRole("button", { name: "新建客服对话" }).click();
      const text = `fixture-status:${status}`;
      await frame.getByLabel("客服问题", { exact: true }).fill(text);
      await frame.getByRole("button", { name: "发送客服问题" }).click();
      await frame.locator(".mini-message.assistant").last().waitFor();
      assert.equal(
        await frame.locator(".mini-message.assistant").last().locator("small").innerText(),
        `小栖 · ${label}`,
      );
    }

    // Error feedback matrix uses fixtures only; only transient/ambiguous failures expose same-request retry.
    const errorCases = [
      [403, "当前账号无法访问这段对话", false],
      [409, "编号与已有内容冲突", false],
      [422, "格式未通过校验", false],
      [429, "发送有些频繁", true],
      [503, "暂时不可用", true],
      [504, "结果暂时未知", true],
      [401, "登录已失效", false],
    ];
    for (const [status, feedback, retryable] of errorCases) {
      if (status !== 401) {
        await frame.getByRole("button", { name: "新建客服对话" }).click();
      }
      else await frame.getByRole('button', { name: '新建客服对话' }).click();
      const text = `fixture-error:${status}`;
      await frame.getByLabel("客服问题", { exact: true }).fill(text);
      await frame.getByRole("button", { name: "发送客服问题" }).click();
      const errorMessage = frame.locator(".mini-message.error").last();
      await errorMessage.waitFor();
      assert.ok((await errorMessage.locator("p").innerText()).includes(feedback), `${status} feedback`);
      assert.equal(await errorMessage.getByRole("button", { name: "重试同一次请求" }).count(), retryable ? 1 : 0);
      if (retryable) {
        const failedRequest = chatRequests.at(-1);
        await errorMessage.getByRole("button", { name: "重试同一次请求" }).click();
        await frame.locator(".mini-message.error").last().waitFor();
        const replay = chatRequests.at(-1);
        assert.equal(replay.headers["idempotency-key"], failedRequest.headers["idempotency-key"]);
        assert.deepEqual(replay.body, failedRequest.body);
      }
      if (status === 401) {
        await frame.getByRole("button", { name: "登录现有账号后咨询" }).waitFor();
      }
    }

    // Compact viewport must keep the launcher and widget within the visible page width.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForTimeout(100);
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
    await page.getByRole("button", { name: "收起小栖客服" }).click();
    assert.equal(await frameElement.count(), 1, "mobile minimize must preserve the iframe");
    await page.getByRole("button", { name: "打开小栖客服" }).click();

    assert.deepEqual(pageErrors, []);
    const goPage = await context.newPage();
    await goPage.route('**/api/**', route => route.fulfill({ status: 599, json: { error: 'unmocked fixture request' } }));
    for (const path of ['/login', '/register']) {
      await goPage.goto(`http://localhost:5173${path}`);
      await goPage.getByRole('button', { name: '打开小栖客服', exact: true }).click();
      const goFrame = goPage.frameLocator('iframe[title="小栖客服对话"]');
      await goFrame.getByRole('button', { name: '登录现有账号后咨询' }).waitFor();
      assert.equal(await goFrame.locator('.mini-message').count(), 0);
      await goFrame.getByRole('button', { name: '收起客服' }).click();
      await goPage.getByRole('button', { name: '打开小栖客服', exact: true }).waitFor();
    }
    await goPage.close();
    const report = {
      status: "passed",
      mocked_auth: true,
      mocked_chat: true,
      demo_flag: true,
      minimize_preserves_iframe_and_draft: true,
      bridge_rejects_bad_origin_and_source: true,
      ime_enter_does_not_send: true,
      late_response_isolated_on_account_change: true,
      validated_citation_navigation: true,
      mobile_viewport: "390x844",
      requests: chatRequests.length,
      page_errors: pageErrors.length,
      real_model_called: false,
      answer_statuses: Object.keys(statusLabels),
      error_statuses: errorCases.map(item => item[0]),
      incomplete_identity_revokes_old_session: true,
      go_host_unauthed_routes: ['/login', '/register'],
    };
    const artifacts = path.resolve(__dirname, '../artifacts');
    await fs.mkdir(artifacts, { recursive: true });
    await fs.writeFile(path.join(artifacts, 'widget-report.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify(report));
    await context.close();
  } finally {
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
