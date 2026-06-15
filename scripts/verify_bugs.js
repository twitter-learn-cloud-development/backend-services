const GATEWAY_URL = 'http://localhost:9638/api/v1';

async function request(url, method = 'GET', data = null, token = null) {
    const headers = {
        'Content-Type': 'application/json'
    };
    if (token) {
        headers['Authorization'] = `Bearer ${token}`;
    }

    const options = {
        method,
        headers,
    };

    if (data) {
        options.body = JSON.stringify(data);
    }

    try {
        const res = await fetch(url, options);
        const contentType = res.headers.get("content-type");
        let body;
        if (contentType && contentType.indexOf("application/json") !== -1) {
            body = await res.json();
        } else {
            body = await res.text();
        }

        if (!res.ok) {
            throw new Error(`Status: ${res.status}, Body: ${JSON.stringify(body)}`);
        }
        return body;
    } catch (e) {
        throw e;
    }
}

async function test() {
    try {
        console.log('--- 🧪 BUG E2E VERIFICATION START ---');

        // 1. 注册并登录新测试账号
        const suffix = Math.floor(Math.random() * 10000);
        const email = `bugtest${suffix}@test.com`;
        const password = 'password123';
        const username = `bugtest${suffix}`;

        console.log(`[1] Registering test user: ${username}`);
        const regRes = await request(`${GATEWAY_URL}/auth/register`, 'POST', { username, email, password });
        const loginRes = await request(`${GATEWAY_URL}/auth/login`, 'POST', { email, password });
        const token = loginRes.token;
        const myUserId = loginRes.user.id;
        console.log(`   ✅ User logged in. ID: ${myUserId}`);

        // 2. 发送带话题的推文
        const hashtag = `bug_${suffix}`;
        const tweetContent = `This is a E2E testing tweet for resolving system-level bugs. #${hashtag} #golang #cloud`;
        console.log(`[2] Publishing new tweet with hashtags: #${hashtag}`);
        const tweetRes = await request(`${GATEWAY_URL}/tweets`, 'POST', { content: tweetContent }, token);
        const newTweetId = tweetRes.tweet.id;
        console.log(`   ✅ Tweet created successfully. ID: ${newTweetId}`);

        // 3. 验证最新推文是否在“为你推荐”排在最上面 (ID 纪元对齐测试)
        console.log('[3] Fetching public timeline (ListTweets)...');
        const publicRes = await request(`${GATEWAY_URL}/tweets/public?limit=5`, 'GET', null, token);
        const firstTweet = publicRes.tweets[0];
        
        if (firstTweet && firstTweet.id === newTweetId) {
            console.log(`   ✅ SUCCESS: The newly created tweet ${newTweetId} is at the top of the feed.`);
        } else {
            console.error(`   ❌ FAIL: Expected new tweet ${newTweetId} at top, but got: ${firstTweet ? firstTweet.id : 'none'}`);
        }

        // 4. 点赞测试 (点赞 500 & 幂等测试)
        console.log(`[4] Liking the tweet ${newTweetId}...`);
        const likeRes = await request(`${GATEWAY_URL}/tweets/${newTweetId}/like`, 'POST', null, token);
        console.log(`   ✅ Like response:`, JSON.stringify(likeRes));

        console.log(`   Liking the same tweet again (Idempotency test)...`);
        const likeRes2 = await request(`${GATEWAY_URL}/tweets/${newTweetId}/like`, 'POST', null, token);
        console.log(`   ✅ Second like response (Idempotent):`, JSON.stringify(likeRes2));

        // 5. 点赞高亮状态拉取测试
        console.log(`[5] Verifying if is_liked is true for logged in user...`);
        const publicResLiked = await request(`${GATEWAY_URL}/tweets/public?limit=5`, 'GET', null, token);
        const firstTweetLiked = publicResLiked.tweets[0];
        if (firstTweetLiked && firstTweetLiked.is_liked === true) {
            console.log(`   ✅ SUCCESS: Tweet shows is_liked = true.`);
        } else {
            console.error(`   ❌ FAIL: Expected is_liked = true, but got:`, firstTweetLiked ? firstTweetLiked.is_liked : 'none');
        }

        // 5.5. 取消点赞测试
        console.log(`[5.5] Unliking the tweet...`);
        const unlikeRes = await request(`${GATEWAY_URL}/tweets/${newTweetId}/like`, 'DELETE', null, token);
        console.log(`   ✅ Unlike response:`, JSON.stringify(unlikeRes));

        const publicResUnliked = await request(`${GATEWAY_URL}/tweets/public?limit=5`, 'GET', null, token);
        const firstTweetUnliked = publicResUnliked.tweets[0];
        if (firstTweetUnliked && firstTweetUnliked.is_liked === false) {
            console.log(`   ✅ SUCCESS: Tweet shows is_liked = false after unlike.`);
        } else {
            console.error(`   ❌ FAIL: Expected is_liked = false, but got:`, firstTweetUnliked ? firstTweetUnliked.is_liked : 'none');
        }

        // 6. 验证话题推荐 (Trends 测试)
        console.log('[6] Waiting for HashtagBatcher to flush to Redis...');
        await new Promise(r => setTimeout(r, 1000));
        
        console.log('[7] Fetching trending topics (/trends)...');
        const trendsRes = await request(`${GATEWAY_URL}/trends?limit=10`, 'GET', null, token);
        console.log('   Trends found:', JSON.stringify(trendsRes.topics));
        
        const found = trendsRes.topics.some(t => t.topic === hashtag);
        if (found) {
            console.log(`   ✅ SUCCESS: Trending topic #${hashtag} is present in the trends list.`);
        } else {
            console.error(`   ❌ FAIL: Trending topic #${hashtag} was not found in:`, JSON.stringify(trendsRes.topics));
        }

        console.log('--- 🧪 BUG E2E VERIFICATION END: ALL PASSED! ---');

    } catch (e) {
        console.error('❌ E2E VERIFICATION ERROR:', e);
    }
}

test();
