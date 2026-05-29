import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  scenarios: {
    attacker: {
      executor: 'constant-vus',
      vus: 50, // 攻击侧 VUs
      duration: '30s',
      exec: 'attack',
    },
    normal_user: {
      executor: 'constant-vus',
      vus: 10, // 防御侧 VUs
      duration: '30s',
      exec: 'read',
    },
  },
  thresholds: {
    'http_req_failed': ['rate<0.05'],
  },
};

const BASE_URL = 'http://localhost:9638';

export function setup() {
  const uniqueId = Math.floor(Math.random() * 1000000);
  const attackerUser = `attacker_${uniqueId}`;
  const readerUser = `reader_${uniqueId}`;

  // 1. 注册并登录攻击者
  http.post(`${BASE_URL}/api/v1/auth/register`, JSON.stringify({
    username: attackerUser,
    email: `${attackerUser}@test.com`,
    password: 'password123',
    nickname: 'Spam Bot'
  }), { headers: { 'Content-Type': 'application/json' } });

  const loginAttacker = http.post(`${BASE_URL}/api/v1/auth/login`, JSON.stringify({
    username: attackerUser,
    password: 'password123'
  }), { headers: { 'Content-Type': 'application/json' } });

  const attackerToken = loginAttacker.json('token') || '';
  const attackerID = loginAttacker.json('user.id') || 0;

  // 2. 注册并登录正常读者
  http.post(`${BASE_URL}/api/v1/auth/register`, JSON.stringify({
    username: readerUser,
    email: `${readerUser}@test.com`,
    password: 'password123',
    nickname: 'Normal Reader'
  }), { headers: { 'Content-Type': 'application/json' } });

  const loginReader = http.post(`${BASE_URL}/api/v1/auth/login`, JSON.stringify({
    username: readerUser,
    password: 'password123'
  }), { headers: { 'Content-Type': 'application/json' } });

  const readerToken = loginReader.json('token') || '';

  // 3. 让读者关注攻击者，以建立写扩散关注链
  if (attackerID && readerToken) {
    http.post(`${BASE_URL}/api/v1/follows`, JSON.stringify({
      followee_id: Number(attackerID)
    }), {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${readerToken}`
      }
    });
  }

  return { attackerToken, readerToken };
}

export function attack(data) {
  const url = `${BASE_URL}/api/v1/tweets`;
  const payload = JSON.stringify({
    content: '澳门线上赌场 终极返利优惠，马上加入！',
    visible_type: 0,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${data.attackerToken}`,
    },
  };

  const res = http.post(url, payload, params);

  check(res, {
    'post status is 200 or 201': (r) => r.status === 200 || r.status === 201,
  });

  sleep(0.5); // 攻击频率 (0.5s)
}

export function read(data) {
  const url = `${BASE_URL}/api/v1/feeds?limit=20`;
  const params = {
    headers: {
      'Authorization': `Bearer ${data.readerToken}`,
    },
  };

  const res = http.get(url, params);
  const body = res.body || '';
  const noSpam = !body.includes('澳门线上赌场');

  check(res, {
    'get status is 200': (r) => r.status === 200,
    'Shadowban effective (No spam in feed)': () => noSpam,
  });

  sleep(2.0); // 正常用户拉取频率 (2.0s)
}
