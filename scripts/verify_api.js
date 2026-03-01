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
        console.log('1. Testing Login...');
        const suffix = Math.floor(Math.random() * 10000);
        const email = `verify${suffix}@test.com`;
        const password = 'password123';
        const username = `verify${suffix}`;

        console.log(`   Registering: ${email} / ${password}`);

        let token = '';
        let userId = '';

        try {
            const regRes = await request(`${GATEWAY_URL}/auth/register`, 'POST', {
                username, email, password
            });
            console.log('   ✅ Register success:', JSON.stringify(regRes));

            const loginRes = await request(`${GATEWAY_URL}/auth/login`, 'POST', {
                email, password
            });
            console.log('   ✅ Login success');
            token = loginRes.token;
            userId = loginRes.user.id;
        } catch (e) {
            console.error('   ❌ Register/Login failed:', e.message);
            return;
        }

        console.log('\n2. Testing GetMe...');
        try {
            const meRes = await request(`${GATEWAY_URL}/users/me`, 'GET', null, token);
            console.log('   ✅ GetMe success:', JSON.stringify(meRes));
        } catch (e) {
            console.error('   ❌ GetMe failed:', e.message);
        }

        console.log(`\n3. Testing GetProfile (ID: ${userId})...`);
        try {
            const profileRes = await request(`${GATEWAY_URL}/users/${userId}`, 'GET', null, token);
            console.log('   ✅ GetProfile success:', JSON.stringify(profileRes));
        } catch (e) {
            console.error('   ❌ GetProfile failed:', e.message);
        }

        console.log('\n4. Testing Create Tweet...');
        try {
            const tweetRes = await request(`${GATEWAY_URL}/tweets`, 'POST', {
                content: "Verification Tweet " + new Date().toISOString()
            }, token);
            console.log('   ✅ Create Tweet success:', JSON.stringify(tweetRes));
        } catch (e) {
            console.error('   ❌ Create Tweet failed:', e.message);
        }

        console.log('\n5. Testing Feeds (Latest)...');
        try {
            // Default limit 20
            const feedsRes = await request(`${GATEWAY_URL}/feeds?cursor=0&limit=10`, 'GET', null, token);
            console.log('   ✅ Feeds success, count:', feedsRes.tweets?.length);
            //  if (feedsRes.tweets?.length === 0) {
            //      console.warn('   ⚠️ Feeds are empty! Check tweet creation.');
            //  }
        } catch (e) {
            console.error('   ❌ Feeds failed:', e.message);
        }

        console.log('\n6. Testing Search (q="Verification")...');
        try {
            // Need to wait a bit for insertion?
            await new Promise(r => setTimeout(r, 1000));
            const searchRes = await request(`${GATEWAY_URL}/search?q=Verification&limit=10`, 'GET', null, token);
            console.log('   ✅ Search success, count:', searchRes.tweets?.length);
        } catch (e) {
            console.error('   ❌ Search failed:', e.message);
        }

        console.log('\n7. Testing Public Timeline (ListTweets)...');
        try {
            const publicRes = await request(`${GATEWAY_URL}/tweets/public?limit=10`, 'GET', null, null);
            console.log('   ✅ Public Timeline success, count:', publicRes.tweets?.length);
            if (!publicRes.tweets || publicRes.tweets.length === 0) {
                console.warn('   ⚠️ Public Timeline is empty! Check DB.');
            }
        } catch (e) {
            console.error('   ❌ Public Timeline failed:', e.message);
        }

    } catch (e) {
        console.error('Unexpected error:', e);
    }
}

test();
