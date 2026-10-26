import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile/features/chat/domain/message_model.dart';
import 'package:mobile/features/chat/presentation/message_history.dart';

MessageModel message(String id, {bool read = true}) => MessageModel(
  id: id,
  senderId: '2',
  receiverId: '1',
  content: 'm$id',
  createdAt: 1,
  isRead: read,
);
Map<String, dynamic> page(
  List<String> ids, [
  String cursor = '',
  bool more = false,
]) => {
  'messages': ids.map(message).toList(),
  'next_cursor': cursor,
  'has_more': more,
};
MessageHistory history(Future<Map<String, dynamic>> Function(String) fetch) =>
    MessageHistory(
      fetch: fetch,
      accepts: (m) => m.senderId == '2' && m.receiverId == '1',
      notify: () {},
    );
void main() {
  test(
    'HTTP and socket deduplicate precise IDs and preserve page-read state',
    () async {
      final pages = [
        page(
          ['9007199254740999', '9007199254740998'],
          '9007199254740998',
          true,
        ),
        page(['9007199254740997']),
      ];
      final h = history((_) async => pages.removeAt(0));
      await h.older();
      h.push(message('9007199254740999', read: false));
      await h.older();
      expect(h.items.map((m) => m.id), [
        '9007199254740999',
        '9007199254740998',
        '9007199254740997',
      ]);
      expect(h.items.first.isRead, true);
    },
  );
  test(
    'recovery budget and continuation preserve anchor independently of live hints and old cursor',
    () async {
      final pages = [
        page(['10', '9'], '9', true),
        page(['20', '19'], '19', true),
        page(['18', '17'], '17', true),
        page(['16', '15'], '15', true),
        page(['14', '13'], '13', true),
        page(['12', '11'], '11', true),
        page(['10', '9'], '9', true),
        page(['8']),
      ];
      final cursors = <String>[];
      final h = history((cursor) async {
        cursors.add(cursor);
        return pages.removeAt(0);
      });
      await h.older();
      h.push(message('21', read: false));
      await h.recover();
      expect(h.continuation, true);
      expect(cursors.length, 4);
      expect(h.cursor, '9');
      await h.recover();
      expect(h.continuation, false);
      await h.older();
      expect(cursors.last, '9');
      expect(h.items.first.id, '21');
    },
  );
  test('failed recovery page retries the same cursor', () async {
    var calls = 0;
    final requested = <String>[];
    final h = history((cursor) async {
      requested.add(cursor);
      calls++;
      if (calls == 1) return page(['5']);
      if (calls == 2) return page(['9', '8'], '8', true);
      if (calls == 3) throw StateError('offline');
      return page(['7', '6', '5']);
    });
    await h.older();
    await h.recover();
    expect(h.error, isNotNull);
    await h.recover();
    expect(requested, ['0', '0', '8', '8']);
  });
  test(
    'disposed conversation ignores late responses and duplicate fetches',
    () async {
      final pending = Completer<Map<String, dynamic>>();
      var calls = 0;
      final h = history((_) {
        calls++;
        return pending.future;
      });
      final first = h.older();
      await h.older();
      await h.recover();
      expect(calls, 1);
      h.dispose();
      pending.complete(page(['5']));
      await first;
      expect(h.items, isEmpty);
    },
  );
  test(
    'bad order and nonadvancing cursor fail without changing items',
    () async {
      for (final result in [
        page(['5', '6']),
        page([], '5', true),
        page(['5'], '4', true),
      ]) {
        final h = history((_) async => result);
        await h.older();
        expect(h.error, isNotNull);
        expect(h.items, isEmpty);
      }
    },
  );
}
