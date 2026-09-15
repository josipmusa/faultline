// The launcher is the only code in the npm packages, and it stands between a
// user and the binary they asked for. Its two jobs are finding the binary and
// reporting faithfully what happened to it.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'

const require = createRequire(import.meta.url)
const { PACKAGE, binaryFile, locate, platformPackage, run } = require('../bin/faultline.js')

// Stands in for require.resolve. The real one is fed by npm having installed
// exactly one platform package.
const resolvesTo = (path) => () => path
const resolvesNothing = () => {
	throw new Error('MODULE_NOT_FOUND')
}

test('platform packages are named after what Node reports', () => {
	assert.equal(platformPackage('linux', 'x64'), 'faultline-proxy-linux-x64')
	assert.equal(platformPackage('darwin', 'arm64'), 'faultline-proxy-darwin-arm64')
	assert.equal(platformPackage('win32', 'x64'), 'faultline-proxy-win32-x64')
})

test('only Windows carries the exe suffix', () => {
	assert.equal(binaryFile('win32'), 'faultline.exe')
	assert.equal(binaryFile('linux'), 'faultline')
	assert.equal(binaryFile('darwin'), 'faultline')
})

test('locate returns the binary inside the platform package', () => {
	assert.equal(locate('linux', 'x64', resolvesTo('/somewhere/faultline')), '/somewhere/faultline')
})

test('a missing platform package explains itself and offers a way out', () => {
	assert.throws(() => locate('linux', 'arm64', resolvesNothing), (err) => {
		// Someone reading this in CI output needs to know which platform was
		// wanted, which package was missing, and what to do instead.
		assert.match(err.message, /linux-arm64/)
		assert.match(err.message, /faultline-proxy-linux-arm64/)
		assert.match(err.message, /install\.sh/)
		return true
	})
})

test('the package name the launcher advertises is the one published', () => {
	assert.equal(PACKAGE, require('../package.json').name)
})

test('the exit code of the binary is the exit code of npx', () => {
	const code = run(['-e', 'process.exit(3)'], 'linux', 'x64', resolvesTo(process.execPath))
	assert.equal(code, 3)
})

test('a binary killed by a signal reports what a shell would have reported', () => {
	// faultline run wraps a long-lived process and is routinely interrupted, so
	// a signalled exit is an ordinary ending here, not an edge case.
	const code = run(['-e', 'process.kill(process.pid, "SIGTERM")'], 'linux', 'x64', resolvesTo(process.execPath))
	assert.equal(code, 143)
})

test('run refuses to invent a binary that is not there', () => {
	assert.throws(() => run([], 'linux', 'x64', resolvesNothing), /not installed/)
})
