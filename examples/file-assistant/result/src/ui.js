const { handleExport } = require('./api');

function downloadExport(user, format) {
  const response = handleExport(user, { format });
  if (response.status !== 200) return { message: response.error };
  return { download: response.filename };
}
module.exports = { downloadExport };
