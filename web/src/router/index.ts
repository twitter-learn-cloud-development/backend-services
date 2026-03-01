import { createRouter, createWebHistory } from 'vue-router'
import Home from '../views/Home.vue'

const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    routes: [
        {
            path: '/',
            name: 'home',
            component: Home
        },
        {
            path: '/login',
            name: 'login',
            component: () => import('../views/Login.vue')
        },
        {
            path: '/register',
            name: 'register',
            component: () => import('../views/Register.vue')
        },
        {
            path: '/explore',
            name: 'explore',
            component: () => import('../views/Explore.vue')
        },
        {
            path: '/notifications',
            name: 'notifications',
            component: () => import('../views/Notifications.vue')
        },
        {
            path: '/messages',
            name: 'messages',
            component: () => import('../views/Messages.vue')
        },
        {
            path: '/bookmarks',
            name: 'bookmarks',
            component: () => import('../views/Bookmarks.vue')
        },
        {
            path: '/profile',
            name: 'profile',
            component: () => import('../views/Profile.vue')
        },
        {
            path: '/users/:id',
            name: 'user-profile',
            component: () => import('../views/Profile.vue')
        },
        {
            path: '/users/:id/followers',
            name: 'user-followers',
            component: () => import('../views/FollowList.vue')
        },
        {
            path: '/users/:id/following',
            name: 'user-following',
            component: () => import('../views/FollowList.vue')
        },
        {
            path: '/tweets/:id',
            name: 'tweet-detail',
            component: () => import('../views/TweetDetail.vue')
        }
    ]
})

export default router
