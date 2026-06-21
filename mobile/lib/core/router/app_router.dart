import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../../features/auth/presentation/login_screen.dart';
import '../../features/auth/presentation/register_screen.dart';
import '../../features/tweet/presentation/tweet_detail_screen.dart';
import '../../features/profile/presentation/profile_screen.dart';
import '../../features/chat/presentation/chat_room_screen.dart';
import '../../shared/presentation/main_layout.dart';
import '../utils/storage.dart';

class AppRouter {
  static final GoRouter router = GoRouter(
    initialLocation: '/',
    redirect: (BuildContext context, GoRouterState state) {
      final token = Storage.getToken();
      final isLoggedIn = token != null && token.isNotEmpty;
      
      final isGoingToLogin = state.matchedLocation == '/login';
      final isGoingToRegister = state.matchedLocation == '/register';

      if (!isLoggedIn) {
        // Not logged in, send to login if not already going to register or login
        if (!isGoingToLogin && !isGoingToRegister) {
          return '/login';
        }
      } else {
        // Logged in, send to home if trying to access login/register
        if (isGoingToLogin || isGoingToRegister) {
          return '/';
        }
      }
      return null;
    },
    routes: [
      GoRoute(
        path: '/',
        builder: (context, state) => const MainLayout(),
      ),
      GoRoute(
        path: '/login',
        builder: (context, state) => const LoginScreen(),
      ),
      GoRoute(
        path: '/register',
        builder: (context, state) => const RegisterScreen(),
      ),
      GoRoute(
        path: '/tweet/:id',
        builder: (context, state) {
          final id = state.pathParameters['id'] ?? '';
          return TweetDetailScreen(tweetId: id);
        },
      ),
      GoRoute(
        path: '/profile/:id',
        builder: (context, state) {
          final id = state.pathParameters['id'] ?? '';
          return ProfileScreen(userId: id);
        },
      ),
      GoRoute(
        path: '/chat/:userId',
        builder: (context, state) {
          final userId = state.pathParameters['userId'] ?? '';
          return ChatRoomScreen(userId: userId);
        },
      ),
    ],
  );
}
