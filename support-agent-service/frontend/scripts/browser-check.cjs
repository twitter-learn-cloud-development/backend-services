// Browser interaction fixtures: these tests never call the model or use real credentials.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE_DIR
    ? path.join(process.env.PLAYWRIGHT_MODULE_DIR, "playwright")
    : "playwright",
);

async function main() {
  const artifacts = path.resolve(__dirname, "../artifacts");
  await fs.mkdir(artifacts, { recursive: true });
  const browser = await chromium.launch({ channel: "msedge", headless: true });
  try {
    const context = await browser.newContext({
      viewport: { width: 1440, height: 900 },
      locale: "zh-CN",
      timezoneId: "Asia/Shanghai",
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto("http://127.0.0.1:5174/");
    await page.getByRole("heading", { name: "有什么想解决的？" }).waitFor();
    await page.screenshot({
      path: path.join(artifacts, "desktop-welcome.png"),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: /看看我的最近发布 查看发布时间/ })
      .click();
    assert.equal(
      await page.locator(".message").count(),
      0,
      "starter should fill the draft, not send",
    );
    await page.getByRole("button", { name: "发送问题", exact: true }).click();
    await page.getByText("下面是一份演示结果", { exact: false }).waitFor();
    await page.screenshot({
      path: path.join(artifacts, "desktop-conversation.png"),
      fullPage: true,
    });
    assert.equal(await page.locator(".message.user").count(), 1);
    await page.getByRole("button", { name: "使用指南" }).click();
    await page.getByRole("dialog").waitFor();
    await page.keyboard.press("Escape");
    assert.equal(await page.getByRole("dialog").count(), 0);
    await page.getByRole("button", { name: /开启新对话/ }).click();
    await page.getByRole("button", { name: "连接账号", exact: true }).click();
    await page.getByRole("button", { name: /真实连接 查询当前账号/ }).click();
    await page.getByRole("button", { name: "关闭连接设置" }).click();
    assert.equal(
      await page.locator(".thread").count(),
      0,
      "demo threads must be isolated from live mode",
    );
    await page.locator("#message-input").fill("前端测试问题");
    await page.getByRole("button", { name: "发送问题", exact: true }).click();
    await page.getByRole("dialog").waitFor();

    await page.route("**/api/v1/auth/login", (route) =>
      route.fulfill({ json: { token: "browser-fixture-token" } }),
    );
    await page.route("**/api/v1/users/me", (route) =>
      route.fulfill({
        json: {
          user: {
            id: "browser-fixture-actor",
            username: "前端测试用户",
            email: "browser@example.test",
          },
        },
      }),
    );
    const captured = [];
    const conversation = "22222222-2222-4222-8222-222222222222";
    await page.route("**/api/v1/support/chat", async (route) => {
      captured.push({
        headers: route.request().headers(),
        body: route.request().postDataJSON(),
      });
      if (captured.length === 2)
        return route.fulfill({
          status: 503,
          json: {
            request_id: "fixture-error",
            code: "temporarily_unavailable",
          },
        });
      return route.fulfill({
        json: {
          request_id: "11111111-1111-4111-8111-111111111111",
          conversation_id: conversation,
          case_id: "33333333-3333-4333-8333-333333333333",
          status: "answered",
          answer: "这是一段浏览器夹具答复，用于检查界面契约。",
          suggested_actions: [],
          evidence_refs: ["fixture-evidence"],
          created_at: "2026-10-02T01:02:03Z",
          trace_id: "fixture-trace",
        },
      });
    });
    await page.getByLabel("账号邮箱").fill("browser@example.test");
    await page
      .getByLabel("密码", { exact: true })
      .fill("fixture-only-password");
    await page.getByRole("button", { name: "登录并开始真实咨询" }).click();
    await page.getByRole("dialog").waitFor({ state: "hidden" });
    assert.equal(
      await page.locator("#message-input").inputValue(),
      "前端测试问题",
      "login must retain an unsent live-mode question",
    );
    await page.locator("#message-input").fill("浏览器夹具首轮");
    await page.getByRole("button", { name: "发送问题", exact: true }).click();
    await page.locator(".message.assistant").waitFor();
    assert.equal("conversation_id" in captured[0].body, false);
    assert.match(captured[0].headers["idempotency-key"], /^[0-9a-f-]{36}$/);
    assert.equal(
      captured[0].headers.authorization,
      "Bearer browser-fixture-token",
    );
    await page.locator("#message-input").fill("浏览器夹具续聊");
    await page.getByRole("button", { name: "发送问题", exact: true }).click();
    await page.getByRole("button", { name: "重试同一次请求" }).waitFor();
    assert.equal(captured[1].body.conversation_id, conversation);
    assert.equal(
      await page
        .locator(".session-card")
        .getByText("答复状态", { exact: true })
        .count(),
      0,
      "stale answer state must not be shown after failure",
    );
    await page.getByRole("button", { name: "重试同一次请求" }).click();
    await page.locator(".message.assistant").nth(1).waitFor();
    assert.equal(
      captured[1].headers["idempotency-key"],
      captured[2].headers["idempotency-key"],
    );
    assert.deepEqual(captured[1].body, captured[2].body);
    assert.equal(
      await page.evaluate(() => localStorage.length + sessionStorage.length),
      0,
    );
    await page.getByRole("button", { name: "连接设置", exact: true }).click();
    await page.getByRole("button", { name: "退出", exact: true }).click();
    assert.equal(
      await page.locator(".message").count(),
      0,
      "logout must clear current account messages",
    );
    assert.equal(
      await page.getByText("前端测试用户", { exact: true }).count(),
      0,
    );
    await context.close();

    const mobile = await browser.newContext({
      viewport: { width: 390, height: 844 },
      isMobile: true,
      deviceScaleFactor: 1,
      locale: "zh-CN",
    });
    const mobilePage = await mobile.newPage();
    mobilePage.on("pageerror", (error) => errors.push(error.message));
    await mobilePage.goto("http://127.0.0.1:5174/");
    await mobilePage
      .getByRole("heading", { name: "有什么想解决的？" })
      .waitFor();
    await mobilePage.screenshot({
      path: path.join(artifacts, "mobile-welcome.png"),
      fullPage: true,
    });
    const noHorizontalOverflow = await mobilePage.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    );
    assert.ok(
      noHorizontalOverflow,
      "mobile viewport must not overflow horizontally",
    );
    await mobilePage.getByRole("button", { name: "打开会话栏" }).click();
    await mobilePage.getByRole("button", { name: "关闭会话栏" }).click();
    await mobilePage.getByRole("button", { name: /了解小栖能做什么/ }).click();
    await mobilePage
      .getByRole("button", { name: "发送问题", exact: true })
      .click();
    await mobilePage.locator(".message.assistant").waitFor();
    await mobilePage.screenshot({
      path: path.join(artifacts, "mobile-conversation.png"),
      fullPage: true,
    });
    // Real, non-mutating check through the Vite proxy: invalid credentials must fail.
    await mobilePage
      .getByRole("button", { name: "连接账号", exact: true })
      .click();
    await mobilePage
      .getByLabel("账号邮箱")
      .fill("frontend-invalid@example.test");
    await mobilePage
      .getByLabel("密码", { exact: true })
      .fill("not-a-real-credential");
    await mobilePage
      .getByRole("button", { name: "登录并开始真实咨询" })
      .click();
    await mobilePage.locator(".login-error").waitFor();
    assert.match(
      await mobilePage.locator(".login-error").innerText(),
      /邮箱或密码不正确/,
    );
    assert.deepEqual(errors, []);
    const report = {
      status: "passed",
      demo: true,
      live_ui_fixture: true,
      mode_isolation: true,
      retry_same_request: true,
      logout_cleanup: true,
      unsent_draft_retained: true,
      actual_gateway_invalid_login_rejected: true,
      desktop: "1440x900",
      mobile: "390x844",
      page_errors: 0,
      real_model_called: false,
    };
    await fs.writeFile(
      path.join(artifacts, "browser-report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify(report));
    await mobile.close();
  } finally {
    await browser.close();
  }
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
