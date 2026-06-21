import 'dart:convert';
import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../utils/storage.dart';
import 'app_config.dart';

// Unread notification count notifier using Riverpod v3 Notifier
class UnreadNotificationNotifier extends Notifier<int> {
  @override
  int build() => 0;

  void increment() {
    state++;
  }

  void set(int value) {
    state = value;
  }
}

final unreadNotificationProvider = NotifierProvider<UnreadNotificationNotifier, int>(UnreadNotificationNotifier.new);

class WebSocketService {
  final Ref _ref;
  WebSocketChannel? _channel;
  bool _isConnecting = false;
  int _retryDelaySeconds = 2;

  // Broadcast Stream for private messages
  final _messageStreamController = StreamController<Map<String, dynamic>>.broadcast();
  Stream<Map<String, dynamic>> get messageStream => _messageStreamController.stream;

  WebSocketService(this._ref);

  void connect() {
    if (_isConnecting || _channel != null) return;
    
    final token = Storage.getToken();
    if (token == null || token.isEmpty) return;

    _isConnecting = true;
    
    // Resolve ws/wss from apiBaseUrl
    final apiBase = AppConfig.apiBaseUrl;
    String wsBase = apiBase.replaceFirst('http://', 'ws://').replaceFirst('https://', 'wss://');
    
    // WS endpoint: ws://host:9638/api/v1/ws?token=<JWT>
    final wsUrl = '$wsBase/ws?token=$token';

    if (kDebugMode) {
      print('🔌 [WebSocket] Connecting to $wsUrl');
    }

    try {
      _channel = WebSocketChannel.connect(Uri.parse(wsUrl));
      _isConnecting = false;
      _retryDelaySeconds = 2; // Reset retry backoff
      
      _channel!.stream.listen(
        (message) {
          _onMessageReceived(message);
        },
        onError: (err) {
          if (kDebugMode) print('❌ [WebSocket] Stream Error: $err');
          _reconnect();
        },
        onDone: () {
          if (kDebugMode) print('🔌 [WebSocket] Connection Closed');
          _reconnect();
        },
      );
    } catch (e) {
      _isConnecting = false;
      if (kDebugMode) print('❌ [WebSocket] Connection Exception: $e');
      _reconnect();
    }
  }

  void _onMessageReceived(dynamic rawMessage) {
    if (kDebugMode) {
      print('📥 [WebSocket] Message Received: $rawMessage');
    }
    
    try {
      final data = jsonDecode(rawMessage.toString()) as Map<String, dynamic>;
      
      // Increment notification badge on likes, comment, or follows
      final type = data['type']?.toString();
      if (type == 'like' || type == 'comment' || type == 'follow') {
        _ref.read(unreadNotificationProvider.notifier).increment();
      } else if (type == 'message') {
        _messageStreamController.add(data);
      }
    } catch (_) {}
  }

  void _reconnect() {
    _closeConnection();
    
    // Exponential backoff reconnect
    Future.delayed(Duration(seconds: _retryDelaySeconds), () {
      if (kDebugMode) {
        print('🔄 [WebSocket] Reconnecting in $_retryDelaySeconds seconds...');
      }
      _retryDelaySeconds = (_retryDelaySeconds * 2).clamp(2, 60);
      connect();
    });
  }

  void _closeConnection() {
    try {
      _channel?.sink.close();
    } catch (_) {}
    _channel = null;
  }

  void disconnect() {
    _closeConnection();
  }

  void dispose() {
    _closeConnection();
    _messageStreamController.close();
  }
}

// Provider for WebSocketService
final websocketServiceProvider = Provider<WebSocketService>((ref) {
  final service = WebSocketService(ref);
  
  // Clean up on provider dispose
  ref.onDispose(() {
    service.dispose();
  });
  
  return service;
});
