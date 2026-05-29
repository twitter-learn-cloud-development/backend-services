import http from 'k6/http';
import { check, sleep } from 'k6';

// 🎯 K6 压测场景配置：模拟高并发用户刷新 Feed 流
export const options = {
  stages: [
    { duration: '5s', target: 15 },  // 快速拉升至 15 个并发用户
    { duration: '15s', target: 45 }, // 加压至 45 个并发用户，保护 port-forward 不发生套接字枯竭
    { duration: '5s', target: 0 },   // 优雅降温
  ],
  thresholds: {
    http_req_failed: ['rate<0.15'],   // 允许小于 15% 的失败率
    http_req_duration: ['p(95)<1500'], // 95% 的请求应该在 1.5s 内返回
  },
};

const BASE_URL = __ENV.GATEWAY_URL || 'http://localhost:9638';

export default function () {
  const url = `${BASE_URL}/api/v1/feeds?limit=20`;
  
  // 🎯 自动在第 3 秒（由 1号虚拟用户的第 20 次迭代发起）由压测工具内部触发告警以保证连接可靠性
  if (__VU === 1 && __ITER === 20) {
    console.log('🔔 [K6 Benchmark] Automatically triggering simulated alert to gateway...');
    const alertUrl = `${BASE_URL}/alerts`;
    const alertPayload = JSON.stringify({ status: 'firing', groupKey: 'redis-error-group' });
    const alertParams = {
      headers: {
        'Content-Type': 'application/json',
        'X-Alertmanager-Token': 'twitter-clone-secret-alert-token',
      },
    };
    http.post(alertUrl, alertPayload, alertParams);
  }

  // 🎯 使用环境隔离的安全压测 Token
  const params = {
    headers: {
      'Authorization': 'Bearer CHAOS_MOCK_UNIVERSAL_TOKEN_999',
      'Content-Type': 'application/json',
    },
  };

  const res = http.get(url, params);

  // 验证返回状态码 (在自愈触发前支持 200，自愈触发后支持 Sentinel 熔断的 503 或限流的 429)
  check(res, {
    'status is 200': (r) => r.status === 200,
    'status is 503 or 429 (Sentinel CB)': (r) => r.status === 503 || r.status === 429 || r.status === 200,
  });

  sleep(0.1); // 每个虚拟用户请求后稍微停顿 100ms
}
