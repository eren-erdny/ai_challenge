// TaskDesk teaching fixture: export authorization and metadata, not a PDF renderer.
function exportTasks(plan, format) {
  if (!['csv', 'pdf'].includes(format)) {
    return { status: 400, error: 'UNSUPPORTED_FORMAT' };
  }
  if (format === 'pdf' && plan !== 'Pro') {
    return { status: 403, error: 'PLAN_REQUIRED' };
  }
  return { status: 200, format, filename: `tasks.${format}` };
}
module.exports = { exportTasks };
