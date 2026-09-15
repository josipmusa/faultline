#!/usr/bin/env node
// Turns a finished GoReleaser build into the npm packages published from it:
// one launcher, and one package per platform carrying a single binary.
//
// It reads dist/metadata.json and dist/artifacts.json, which GoReleaser writes
// for every build, rather than guessing at the layout of dist/ - the directory
// names there carry microarchitecture suffixes that change with the build
// settings.
//
//   node npm/build.mjs [dist-dir] [out-dir]
//
// Publishing is left to the caller: the platform packages have to go up before
// the launcher that depends on them.

import { chmodSync, copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))

// The process.platform and process.arch spellings of Go's GOOS and GOARCH.
// The launcher builds a package name out of those two at runtime, so these are
// the names it will look for.
const PLATFORMS = { linux: 'linux', darwin: 'darwin', windows: 'win32' }
const ARCHES = { amd64: 'x64', arm64: 'arm64' }

export function npmTarget(goos, goarch) {
	const platform = PLATFORMS[goos]
	const arch = ARCHES[goarch]
	return platform && arch ? { platform, arch } : null
}

export function packageName(base, platform, arch) {
	return `${base}-${platform}-${arch}`
}

// The launcher's package.json is checked in, so its metadata is edited in one
// place and everything generated here inherits it.
function template() {
	return JSON.parse(readFileSync(join(HERE, 'package.json'), 'utf8'))
}

export function readBuild(distDir) {
	const metadata = JSON.parse(readFileSync(join(distDir, 'metadata.json'), 'utf8'))
	const artifacts = JSON.parse(readFileSync(join(distDir, 'artifacts.json'), 'utf8'))

	const binaries = []
	for (const artifact of artifacts) {
		if (artifact.type !== 'Binary') continue
		const target = npmTarget(artifact.goos, artifact.goarch)
		if (!target) {
			// Loud rather than silent: a platform npm cannot express is a
			// decision for a person, not something to drop on the floor.
			console.warn(`warning: no npm target for ${artifact.goos}/${artifact.goarch}, skipping`)
			continue
		}
		binaries.push({ ...target, name: artifact.name, path: artifact.path })
	}

	if (binaries.length === 0) {
		throw new Error(`no publishable binaries in ${distDir}: run goreleaser first`)
	}
	return { version: metadata.version, binaries }
}

function writePlatformPackage(outDir, base, version, binary, meta) {
	const name = packageName(base, binary.platform, binary.arch)
	const dir = join(outDir, name)
	mkdirSync(join(dir, 'bin'), { recursive: true })

	copyFileSync(binary.path, join(dir, 'bin', binary.name))
	// The archive that npm ships does record the mode, and the launcher spawns
	// this file directly, so it has to arrive executable.
	chmodSync(join(dir, 'bin', binary.name), 0o755)

	writeFileSync(
		join(dir, 'package.json'),
		JSON.stringify(
			{
				name,
				version,
				description: `${binary.platform}-${binary.arch} binary for ${base}.`,
				homepage: meta.homepage,
				repository: meta.repository,
				license: meta.license,
				// os and cpu are what make this package optional everywhere
				// else: npm installs only the one that matches the machine.
				os: [binary.platform],
				cpu: [binary.arch],
				// Yarn keeps packages zipped under Plug'n'Play, and a zipped
				// binary cannot be executed.
				preferUnplugged: true,
				files: ['bin'],
			},
			null,
			2
		) + '\n'
	)
	writeFileSync(
		join(dir, 'README.md'),
		`# ${name}\n\nThe ${binary.platform}-${binary.arch} binary for [${base}](https://www.npmjs.com/package/${base}).\n\n` +
			`Install \`${base}\` instead; npm picks this package for you.\n`
	)
	return name
}

function writeLauncher(outDir, base, version, dependencies) {
	const dir = join(outDir, base)
	mkdirSync(join(dir, 'bin'), { recursive: true })

	const pkg = template()
	pkg.version = version
	// Pinned exactly. A launcher paired with a different build's binary is a
	// bug nobody would enjoy finding.
	pkg.optionalDependencies = Object.fromEntries(dependencies.sort().map((name) => [name, version]))
	// The test script refers to files this package does not ship.
	delete pkg.scripts

	writeFileSync(join(dir, 'package.json'), JSON.stringify(pkg, null, 2) + '\n')
	copyFileSync(join(HERE, 'bin', 'faultline.js'), join(dir, 'bin', 'faultline.js'))
	chmodSync(join(dir, 'bin', 'faultline.js'), 0o755)
	copyFileSync(join(HERE, 'README.md'), join(dir, 'README.md'))
	return dir
}

export function build({ distDir, outDir }) {
	const { version, binaries } = readBuild(distDir)
	const base = template().name

	rmSync(outDir, { recursive: true, force: true })
	mkdirSync(outDir, { recursive: true })

	const meta = template()
	const names = binaries.map((binary) => writePlatformPackage(outDir, base, version, binary, meta))
	writeLauncher(outDir, base, version, names)

	return { version, base, platforms: names }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	const distDir = process.argv[2] || join(HERE, '..', 'dist')
	const outDir = process.argv[3] || join(HERE, 'dist')
	const { version, base, platforms } = build({ distDir, outDir })
	console.log(`built ${base}@${version} and ${platforms.length} platform packages in ${outDir}`)
	for (const name of platforms) console.log(`  ${name}`)
}
