// The npm packages are assembled once, on a tag, by a job nobody watches. A
// mistake here reaches users as a package that installs and then cannot run,
// so the assembly is exercised against a stand-in for GoReleaser's output.
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

import { build, npmTarget, packageName, readBuild } from '../build.mjs'

const TARGETS = [
	['linux', 'amd64', 'linux', 'x64'],
	['linux', 'arm64', 'linux', 'arm64'],
	['darwin', 'amd64', 'darwin', 'x64'],
	['darwin', 'arm64', 'darwin', 'arm64'],
	['windows', 'amd64', 'win32', 'x64'],
	['windows', 'arm64', 'win32', 'arm64'],
]

// Writes what GoReleaser leaves in dist/ for the targets given.
function fakeDist(version, targets) {
	const dir = mkdtempSync(join(tmpdir(), 'faultline-dist-'))
	writeFileSync(join(dir, 'metadata.json'), JSON.stringify({ version, tag: `v${version}` }))

	const artifacts = targets.map(([goos, goarch]) => {
		const name = goos === 'windows' ? 'faultline.exe' : 'faultline'
		const path = join(dir, `faultline_${goos}_${goarch}`, name)
		mkdirSync(join(dir, `faultline_${goos}_${goarch}`), { recursive: true })
		writeFileSync(path, `binary for ${goos}/${goarch}`)
		return { name, path, goos, goarch, type: 'Binary' }
	})
	// GoReleaser lists more than binaries; the archives must be ignored.
	artifacts.push({ name: 'faultline_1.2.3_linux_amd64.tar.gz', path: 'x', goos: 'linux', goarch: 'amd64', type: 'Archive' })
	artifacts.push({ name: 'checksums.txt', path: 'y', type: 'Checksum' })

	writeFileSync(join(dir, 'artifacts.json'), JSON.stringify(artifacts))
	return dir
}

test('npmTarget maps Go names to the ones Node reports', () => {
	for (const [goos, goarch, platform, arch] of TARGETS) {
		assert.deepEqual(npmTarget(goos, goarch), { platform, arch })
	}
})

test('npmTarget refuses a platform npm cannot express', () => {
	assert.equal(npmTarget('plan9', 'amd64'), null)
	assert.equal(npmTarget('linux', 'riscv64'), null)
})

test('packageName is the name the launcher looks for at runtime', () => {
	assert.equal(packageName('faultline-proxy', 'darwin', 'arm64'), 'faultline-proxy-darwin-arm64')
})

test('readBuild takes the binaries and leaves the archives', () => {
	const { version, binaries } = readBuild(fakeDist('1.2.3', TARGETS))
	assert.equal(version, '1.2.3')
	assert.equal(binaries.length, 6)
	assert.ok(binaries.every((b) => b.name === 'faultline' || b.name === 'faultline.exe'))
})

test('readBuild fails rather than publishing nothing', () => {
	assert.throws(() => readBuild(fakeDist('1.2.3', [['plan9', 'amd64']])), /no publishable binaries/)
})

test('build writes one runnable package per platform', () => {
	const outDir = mkdtempSync(join(tmpdir(), 'faultline-npm-'))
	const { platforms } = build({ distDir: fakeDist('1.2.3', TARGETS), outDir })

	assert.equal(platforms.length, 6)
	for (const [goos, goarch, platform, arch] of TARGETS) {
		const name = `faultline-proxy-${platform}-${arch}`
		const pkg = JSON.parse(readFileSync(join(outDir, name, 'package.json'), 'utf8'))

		assert.equal(pkg.version, '1.2.3')
		// os and cpu are the whole mechanism: without them npm would install
		// all six platform packages on every machine.
		assert.deepEqual(pkg.os, [platform])
		assert.deepEqual(pkg.cpu, [arch])
		assert.equal(pkg.preferUnplugged, true)
		assert.ok(!pkg.exports, 'an exports field would break the deep require.resolve the launcher does')

		const binary = join(outDir, name, 'bin', goos === 'windows' ? 'faultline.exe' : 'faultline')
		assert.equal(readFileSync(binary, 'utf8'), `binary for ${goos}/${goarch}`)
		assert.equal(statSync(binary).mode & 0o111, 0o111, 'binary must arrive executable')
	}
})

test('build pins the launcher to exactly the binaries built beside it', () => {
	const outDir = mkdtempSync(join(tmpdir(), 'faultline-npm-'))
	build({ distDir: fakeDist('1.2.3', TARGETS), outDir })

	const pkg = JSON.parse(readFileSync(join(outDir, 'faultline-proxy', 'package.json'), 'utf8'))
	assert.equal(pkg.version, '1.2.3')
	assert.deepEqual(pkg.bin, { faultline: 'bin/faultline.js' })
	assert.equal(Object.keys(pkg.optionalDependencies).length, 6)
	for (const version of Object.values(pkg.optionalDependencies)) {
		assert.equal(version, '1.2.3', 'a launcher must never pair with another build’s binary')
	}
	assert.ok(!pkg.scripts, 'the test script refers to files this package does not ship')
	assert.equal(statSync(join(outDir, 'faultline-proxy', 'bin', 'faultline.js')).mode & 0o111, 0o111)
})

test('build only ships the launcher and the binaries', () => {
	const outDir = mkdtempSync(join(tmpdir(), 'faultline-npm-'))
	build({ distDir: fakeDist('1.2.3', TARGETS), outDir })

	const pkg = JSON.parse(readFileSync(join(outDir, 'faultline-proxy', 'package.json'), 'utf8'))
	assert.deepEqual(pkg.files, ['bin'])
	assert.equal(pkg.dependencies, undefined, 'the launcher must stay dependency free')
})
