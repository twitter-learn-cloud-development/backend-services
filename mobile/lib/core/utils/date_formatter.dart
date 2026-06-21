import 'package:intl/intl.dart';

class DateFormatter {
  static String formatRelative(int timestampSeconds) {
    if (timestampSeconds == 0) return '';
    
    final dateTime = DateTime.fromMillisecondsSinceEpoch(timestampSeconds * 1000);
    final now = DateTime.now();
    final difference = now.difference(dateTime);

    if (difference.inSeconds < 60) {
      return '刚刚';
    } else if (difference.inMinutes < 60) {
      return '${difference.inMinutes}分';
    } else if (difference.inHours < 24) {
      return '${difference.inHours}小时';
    } else if (difference.inDays < 7) {
      return '${difference.inDays}天';
    } else {
      return DateFormat('M月d日').format(dateTime);
    }
  }
}
