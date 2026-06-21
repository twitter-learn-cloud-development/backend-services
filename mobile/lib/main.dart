import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/router/app_router.dart';
import 'core/theme/app_theme.dart';
import 'core/utils/storage.dart';
import 'core/network/dio_client.dart';

void main() async {
  // Ensure Flutter engine bindings are initialized
  WidgetsFlutterBinding.ensureInitialized();
  
  // Initialize storage
  await Storage.init();

  // Run the app with Riverpod Scope
  runApp(
    const ProviderScope(
      child: MyApp(),
    ),
  );
}

class MyApp extends ConsumerWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Optionally: Listen to theme provider or local storage theme setting
    final storedTheme = Storage.getThemeMode();
    ThemeMode themeMode = ThemeMode.system;
    
    if (storedTheme == 'light') {
      themeMode = ThemeMode.light;
    } else if (storedTheme == 'dark') {
      themeMode = ThemeMode.dark;
    }

    return MaterialApp.router(
      title: 'Twitter Clone',
      debugShowCheckedModeBanner: false,
      
      // Themes
      theme: AppTheme.lightTheme,
      darkTheme: AppTheme.darkTheme,
      themeMode: themeMode,
      
      // Router
      routerConfig: AppRouter.router,
    );
  }
}
