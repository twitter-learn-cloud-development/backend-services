import 'dart:convert';
import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../utils/storage.dart';
import 'app_config.dart';
import '../../features/auth/presentation/auth_notifier.dart';

// Unread notification count notifier using Riverpod v3 Notifier
class UnreadNotificationNotifier extends Notifier<int> {
  @override
  int build() {
    ref.watch(authNotifierProvider.select((auth) => auth.token));
    return 0;
  }

  void increment() {
    state++;
  }

  void set(int value) {
    state = value;
  }
}

final unreadNotificationProvider =
    NotifierProvider<UnreadNotificationNotifier, int>(
      UnreadNotificationNotifier.new,
    );

class WebSocketService {
  final Ref _ref;
  final WebSocketChannel Function(Uri) _channelFactory;
  WebSocketChannel? _channel;
  bool _isConnecting = false;
  int _retryDelaySeconds = 2;
  Timer? _retry;
  bool _enabled = false;
  bool _disposed = false;
  int _generation = 0;

  // Broadcast Stream for private messages
  final _messageStreamController =
      StreamController<Map<String, dynamic>>.broadcast();
  Stream<Map<String, dynamic>> get messageStream =>
      _messageStreamController.stream;
  final _readyController = StreamController<void>.broadcast();
  Stream<void> get readyStream => _readyController.stream;

  WebSocketService(this._ref, {WebSocketChannel Function(Uri)? channelFactory})
    : _channelFactory = channelFactory ?? WebSocketChannel.connect;

  void connect() {
    if (_disposed) return;
    _enabled = true;
    _retry?.cancel();
    if (_isConnecting || _channel != null) return;

    final token = Storage.getToken();
    if (token == null || token.isEmpty) return;

    _isConnecting = true;

    // Resolve ws/wss from apiBaseUrl
    final apiBase = AppConfig.apiBaseUrl;
    String wsBase = apiBase
        .replaceFirst('http://', 'ws://')
        .replaceFirst('https://', 'wss://');

    // WS endpoint: ws://host:9638/api/v1/ws?token=<JWT>
    final wsUrl = '$wsBase/ws?token=$token';

    if (kDebugMode) {
      print('🔌 [WebSocket] Connecting');
    }

    try {
      final generation = ++_generation;
      _channel = _channelFactory(Uri.parse(wsUrl));
      _isConnecting = false;
      final channel = _channel!;
      channel.ready.then(
        (_) {
          if (_enabled && !_disposed && generation == _generation) {
            _retryDelaySeconds = 2;
            _readyController.add(null);
          }
        },
        onError: (Object _) {
          if (_enabled && generation == _generation) _reconnect();
        },
      );

      _channel!.stream.listen(
        (message) {
          if (_enabled && generation == _generation) {
            _onMessageReceived(message);
          }
        },
        onError: (err) {
          if (kDebugMode) print('❌ [WebSocket] Stream Error');
          if (_enabled && generation == _generation) _reconnect();
        },
        onDone: () {
          if (kDebugMode) print('🔌 [WebSocket] Connection Closed');
          if (_enabled && generation == _generation) _reconnect();
        },
      );
    } catch (e) {
      _isConnecting = false;
      if (kDebugMode) print('❌ [WebSocket] Connection Exception');
      _reconnect();
    }
  }

  void _onMessageReceived(dynamic rawMessage) {
    try {
      final data = jsonDecode(rawMessage.toString()) as Map<String, dynamic>;

      // Increment notification badge on likes, comment, or follows
      final type = data['type']?.toString();
      if (type == 'ready') {
        _readyController.add(null);
      } else if (type == 'like' || type == 'comment' || type == 'follow') {
        _ref.read(unreadNotificationProvider.notifier).increment();
      } else if (type == 'message') {
        _messageStreamController.add(data);
      }
    } catch (_) {}
  }

  void _reconnect() {
    if (!_enabled || _disposed) return;
    _closeConnection();

    // Exponential backoff reconnect
    _retry?.cancel();
    _retry = Timer(Duration(seconds: _retryDelaySeconds), () {
      if (!_enabled || _disposed) return;
      if (kDebugMode) {
        print('🔄 [WebSocket] Reconnecting in $_retryDelaySeconds seconds...');
      }
      _retryDelaySeconds = (_retryDelaySeconds * 2).clamp(2, 60);
      connect();
    });
  }

  void _closeConnection() {
    ++_generation;
    try {
      _channel?.sink.close();
    } catch (_) {}
    _channel = null;
  }

  void disconnect() {
    _enabled = false;
    _retry?.cancel();
    _closeConnection();
  }

  void dispose() {
    _disposed = true;
    disconnect();
    _messageStreamController.close();
    _readyController.close();
  }
}

// Provider for WebSocketService
final websocketServiceProvider = Provider<WebSocketService>((ref) {
  final service = WebSocketService(ref);
  ref.listen(authNotifierProvider.select((auth) => auth.token), (_, next) {
    service.disconnect();
    if (next != null && next.isNotEmpty) service.connect();
  });

  // Clean up on provider dispose
  ref.onDispose(() {
    service.dispose();
  });

  return service;
});
