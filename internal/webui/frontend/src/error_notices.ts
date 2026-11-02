import type { ErrorNotice } from './types';

// Errors contain reviewed messages only, never caught exceptions, binding
// values or provider response bodies. Keep the queue bounded and expose
// overflow explicitly instead of silently discarding an unbounded stream.
export function appendErrorNotice(queue: ErrorNotice[], notice: ErrorNotice): ErrorNotice[] {
  const last = queue[queue.length - 1];
  if (last?.Code === 'ERROR_QUEUE_OVERFLOW') return queue;
  if (last && last.Title === notice.Title && last.Operation === notice.Operation && last.Code === notice.Code && last.Message === notice.Message) return queue;
  if (queue.length < 31) return [...queue, notice];
  return [...queue, { ID: notice.ID.slice(0, 55) + '-overflow', Title: 'Additional errors', Operation: 'Error reporting', Code: 'ERROR_QUEUE_OVERFLOW', Message: 'Additional errors occurred while earlier messages were waiting. They have been grouped here. Review the listed errors, then retry the affected operation when ready.' }];
}
