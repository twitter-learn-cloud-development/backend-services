import '../domain/message_model.dart';

int compareMessageIds(String a, String b) =>
    a.length != b.length ? a.length.compareTo(b.length) : a.compareTo(b);

bool _validMessageId(String id) =>
    RegExp(r'^[1-9][0-9]{0,19}$').hasMatch(id) &&
    compareMessageIds(id, '18446744073709551615') <= 0;

class MessageHistory {
  final Future<Map<String, dynamic>> Function(String) fetch;
  final bool Function(MessageModel) accepts;
  final void Function() notify;
  List<MessageModel> items = [];
  String cursor = '0', _anchor = '0', _syncCursor = '0', _syncHead = '0';
  bool hasMore = true,
      busy = false,
      syncing = false,
      continuation = false,
      _disposed = false;
  String? error;
  MessageHistory({
    required this.fetch,
    required this.accepts,
    required this.notify,
  });

  void _merge(List<MessageModel> rows) {
    final merged = {for (final item in items) item.id: item};
    for (final row in rows) {
      if (!_validMessageId(row.id) || !accepts(row)) {
        throw const FormatException('invalid message identity');
      }
      final old = merged[row.id];
      merged[row.id] = MessageModel(
        id: row.id,
        senderId: row.senderId,
        receiverId: row.receiverId,
        content: row.content,
        createdAt: row.createdAt,
        isRead: row.isRead || (old?.isRead ?? false),
        sender: row.sender ?? old?.sender,
      );
    }
    items = merged.values.toList()
      ..sort((a, b) => compareMessageIds(b.id, a.id));
  }

  void push(MessageModel row) {
    if (!_disposed && accepts(row)) {
      _merge([row]);
      notify();
    }
  }

  List<MessageModel> _validate(Map<String, dynamic> page, String before) {
    final rows = page['messages'] as List<MessageModel>;
    if (rows.length > 100 ||
        page['has_more'] is! bool ||
        page['next_cursor'] is! String) {
      throw const FormatException('invalid page');
    }
    var previous = before;
    for (final row in rows) {
      if (!_validMessageId(row.id) ||
          !accepts(row) ||
          (previous != '0' && compareMessageIds(row.id, previous) >= 0)) {
        throw const FormatException('invalid order');
      }
      previous = row.id;
    }
    if (page['has_more'] == true &&
        (rows.isEmpty || page['next_cursor'] != previous)) {
      throw const FormatException('cursor did not advance');
    }
    return rows;
  }

  Future<void> older() async {
    if (_disposed || busy || syncing || !hasMore) return;
    busy = true;
    error = null;
    notify();
    final before = cursor;
    try {
      final page = await fetch(before);
      if (_disposed) return;
      final rows = _validate(page, before);
      _merge(rows);
      cursor = (page['next_cursor'] as String).isEmpty
          ? '0'
          : page['next_cursor'];
      hasMore = page['has_more'];
      if (before == '0') _anchor = rows.isEmpty ? '0' : rows.first.id;
    } catch (_) {
      if (!_disposed) error = '历史加载失败，请重试';
    } finally {
      if (!_disposed) {
        busy = false;
        notify();
      }
    }
  }

  Future<void> recover() async {
    if (_disposed || busy || syncing) return;
    if (cursor == '0' && items.isEmpty && hasMore) {
      await older();
      return;
    }
    syncing = true;
    error = null;
    notify();
    try {
      for (var i = 0; i < 3; i++) {
        final before = _syncCursor, page = await fetch(_syncCursor);
        if (_disposed) return;
        final rows = _validate(page, before);
        if (before == '0') _syncHead = rows.isEmpty ? _anchor : rows.first.id;
        _merge(rows);
        final reached =
            _anchor != '0' &&
            rows.any((m) => compareMessageIds(m.id, _anchor) <= 0);
        if (reached || page['has_more'] == false) {
          _anchor = _syncHead;
          _syncCursor = '0';
          continuation = false;
          return;
        }
        _syncCursor = page['next_cursor'];
        continuation = true;
      }
    } catch (_) {
      if (!_disposed) error = '消息同步失败，继续同步可重试';
    } finally {
      if (!_disposed) {
        syncing = false;
        notify();
      }
    }
  }

  void dispose() {
    _disposed = true;
  }
}
