const { test } = require('node:test');
const assert = require('node:assert/strict');
const { exportTasks } = require('../src/export');
const { handleExport } = require('../src/api');
const { downloadExport } = require('../src/ui');

test('Free can export CSV', () => assert.equal(exportTasks('Free', 'csv').status, 200));
test('Free cannot export PDF', () => assert.deepEqual(exportTasks('Free', 'pdf'), { status: 403, error: 'PLAN_REQUIRED' }));
test('Pro can export CSV', () => assert.equal(exportTasks('Pro', 'csv').status, 200));
test('Pro can export PDF', () => assert.equal(exportTasks('Pro', 'pdf').filename, 'tasks.pdf'));
test('Unsupported format is a client error', () => assert.equal(exportTasks('Pro', 'xml').status, 400));
test('API delegates plan checks', () => assert.equal(handleExport({plan: 'Free'}, {format: 'pdf'}).error, 'PLAN_REQUIRED'));
test('UI reports API failure', () => assert.equal(downloadExport({plan: 'Free'}, 'pdf').message, 'PLAN_REQUIRED'));
