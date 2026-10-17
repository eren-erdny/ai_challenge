const { exportTasks } = require('./export');

// POST /api/export; authenticated user identity is supplied by the host application.
function handleExport(user, body) {
  return exportTasks(user.plan, body.format);
}
module.exports = { handleExport };
