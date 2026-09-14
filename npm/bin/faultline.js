#!/usr/bin/env node
// The npm face of Faultline. Everything real is in the Go binary; this only
// finds the right one and gets out of the way.
//
// There are no dependencies and no postinstall script on purpose. The binary
// arrives inside a platform package that npm picks by the os and cpu fields it
// declares, so `--ignore-scripts` installs, private registry mirrors and
// offline caches all keep working, and a machine downloads one binary rather
// than six.
'use strict'

const { spawnSync } = require('node:child_process')
const os = require('node:os')

const PACKAGE = 'faultline-proxy'
const INSTALLER = 'https://raw.githubusercontent.com/josipmusa/faultline/main/install.sh'

// Platform packages are named after process.platform and process.arch, which
// is what lets this find one without carrying a lookup table.
function platformPackage(platform, arch) {
	return `${PACKAGE}-${platform}-${arch}`
}

function binaryFile(platform) {
	return platform === 'win32' ? 'faultline.exe' : 'faultline'
}

// resolve is a parameter so the not-installed path can be exercised in a test
// without uninstalling anything. In a real run it is require.resolve.
function locate(platform, arch, resolve) {
	const pkg = platformPackage(platform, arch)
	try {
		return resolve(`${pkg}/bin/${binaryFile(platform)}`)
	} catch {
		throw new Error(
			`${PACKAGE} has no binary for ${platform}-${arch}: the optional dependency ` +
				`${pkg} is not installed.\n` +
				'Either Faultline does not publish this platform, or the install skipped ' +
				'optional dependencies.\n' +
				`Installing directly works anywhere Faultline runs:\n  curl -fsSL ${INSTALLER} | sh`
		)
	}
}

// Returns the exit code the caller should exit with.
function run(argv, platform, arch, resolve) {
	const binary = locate(platform, arch, resolve)
	// stdio is inherited, never piped: `faultline run` wraps another process
	// whose output is the whole point, and a pipe would buffer it and hide the
	// fact that it is talking to a terminal.
	const result = spawnSync(binary, argv, { stdio: 'inherit' })
	if (result.error) {
		throw result.error
	}
	// A process killed by a signal reports no exit status. 128 plus the signal
	// number is what the shell would have reported had it run the binary.
	if (result.signal) {
		return 128 + (os.constants.signals[result.signal] || 0)
	}
	return result.status === null ? 1 : result.status
}

if (require.main === module) {
	try {
		process.exit(run(process.argv.slice(2), process.platform, process.arch, (request) => require.resolve(request)))
	} catch (err) {
		console.error(err.message)
		process.exit(1)
	}
}

module.exports = { PACKAGE, platformPackage, binaryFile, locate, run }
