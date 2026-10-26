import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../features/auth/presentation/auth_notifier.dart';
import '../../features/auth/presentation/account_security_screen.dart';
import '../../features/auth/presentation/login_screen.dart';
import '../../features/auth/presentation/register_screen.dart';
import '../../features/tweet/presentation/tweet_detail_screen.dart';
import '../../features/profile/presentation/profile_screen.dart';
import '../../features/chat/presentation/chat_room_screen.dart';
import '../../shared/presentation/main_layout.dart';

final appRouterProvider = Provider<GoRouter>((ref) {
  final loggedIn = ValueNotifier(
    ref.read(authNotifierProvider).isAuthenticated,
  );
  ref.listen(authNotifierProvider.select((auth) => auth.isAuthenticated), (
    _,
    next,
  ) {
    loggedIn.value = next;
  });
  final router = GoRouter(
    refreshListenable: loggedIn,
    initialLocation: '/',
    redirect: (BuildContext context, GoRouterState state) {
      final isLoggedIn = loggedIn.value;

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
        path: '/account/security',
        builder: (context, state) => const AccountSecurityScreen(),
      ),
      GoRoute(path: '/', builder: (context, state) => const MainLayout()),
      GoRoute(path: '/login', builder: (context, state) => const LoginScreen()),
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
  ref.onDispose(() {
    router.dispose();
    loggedIn.dispose();
  });
  return router;
});
