import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../features/tweet/presentation/home_screen.dart';
import '../../features/search/presentation/search_screen.dart';
import '../../features/agent/presentation/agent_chat_screen.dart';
import '../../features/notification/presentation/notification_screen.dart';
import '../../features/chat/presentation/chat_list_screen.dart';
import '../../features/tweet/presentation/compose_screen.dart';
import '../../core/constants/colors.dart';
import '../../core/network/websocket_service.dart';

class MainLayout extends ConsumerStatefulWidget {
  const MainLayout({super.key});

  @override
  ConsumerState<MainLayout> createState() => _MainLayoutState();
}

class _MainLayoutState extends ConsumerState<MainLayout> {
  int _currentIndex = 0;

  final List<Widget> _screens = [
    const HomeScreen(),
    const SearchScreen(),
    const AgentChatScreen(),
    const NotificationScreen(),
    const ChatListScreen(),
  ];

  @override
  void initState() {
    super.initState();
    // Start WebSocket real-time notification engine
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(websocketServiceProvider).connect();
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;
    
    // Watch unread notifications count
    final unreadCount = ref.watch(unreadNotificationProvider);

    return Scaffold(
      body: IndexedStack(
        index: _currentIndex,
        children: _screens,
      ),
      bottomNavigationBar: Container(
        decoration: BoxDecoration(
          border: Border(
            top: BorderSide(
              color: isDark ? AppColors.darkBorder : AppColors.lightBorder,
              width: 0.5,
            ),
          ),
        ),
        child: BottomNavigationBar(
          currentIndex: _currentIndex,
          onTap: (index) {
            setState(() {
              _currentIndex = index;
            });
            // Clear notifications count when viewing notification screen
            if (index == 3) {
              ref.read(unreadNotificationProvider.notifier).set(0);
            }
          },
          items: [
            const BottomNavigationBarItem(
              icon: Icon(Icons.home_outlined),
              activeIcon: Icon(Icons.home),
              label: '首页',
            ),
            const BottomNavigationBarItem(
              icon: Icon(Icons.search_outlined),
              activeIcon: Icon(Icons.search),
              label: '探索',
            ),
            const BottomNavigationBarItem(
              icon: Icon(Icons.smart_toy_outlined),
              activeIcon: Icon(Icons.smart_toy),
              label: 'AI助手',
            ),
            BottomNavigationBarItem(
              icon: Badge(
                isLabelVisible: unreadCount > 0,
                label: Text(unreadCount.toString()),
                child: const Icon(Icons.notifications_outlined),
              ),
              activeIcon: Badge(
                isLabelVisible: unreadCount > 0,
                label: Text(unreadCount.toString()),
                child: const Icon(Icons.notifications),
              ),
              label: '通知',
            ),
            const BottomNavigationBarItem(
              icon: Icon(Icons.mail_outlined),
              activeIcon: Icon(Icons.mail),
              label: '私信',
            ),
          ],
        ),
      ),
      floatingActionButton: _currentIndex == 0 || _currentIndex == 4
          ? FloatingActionButton(
              onPressed: () {
                Navigator.of(context).push(
                  MaterialPageRoute(builder: (_) => const ComposeScreen()),
                );
              },
              backgroundColor: AppColors.primary,
              shape: const CircleBorder(),
              child: Icon(
                _currentIndex == 4 ? Icons.mail_outline : Icons.add,
                color: Colors.white,
                size: 28,
              ),
            )
          : null,
    );
  }
}
