// @ts-check
"use strict"

const crypto = require("node:crypto")
const fs = require("node:fs")
const path = require("node:path")

const TAG_RE = /^v0\.[0-9]{4}\.[0-9]+$/
const EXTRA_FILES = ["LICENSE", "NOTICE", "THIRD_PARTY_LICENSES"]
const SBOM_NAME = "lm.spdx.json"
const SUMS_NAME = "SHA256SUMS"

function resolveVersion({ eventName, refName, sha }) {
  if (eventName === "push") {
    if (!TAG_RE.test(refName)) {
      throw new Error(`tag ${refName} is not a CalVer release tag`)
    }
    return refName
  }
  return `v0.0.0-dryrun+${sha.slice(0, 12)}`
}

function parseModules(goVersionM, version) {
  const modules = []
  for (const line of goVersionM.split("\n")) {
    const f = line.split("\t")
    if (f[1] === "mod") {
      modules.push({ path: f[2], version, hash: "" })
    } else if (f[1] === "dep") {
      modules.push({ path: f[2], version: f[3], hash: f[4] || "" })
    } else if (f[1] === "=>" && modules.length > 0) {
      modules[modules.length - 1] = { path: f[2], version: f[3], hash: f[4] || "" }
    }
  }
  if (modules.length === 0) {
    throw new Error("go version -m printed no modules")
  }
  return modules
}

function createdFromEpoch(epoch) {
  const seconds = Number(epoch)
  if (epoch === "" || !Number.isInteger(seconds)) {
    throw new Error(`SOURCE_DATE_EPOCH ${epoch} is not an integer`)
  }
  return new Date(seconds * 1000).toISOString().replace(/\.\d{3}Z$/, "Z")
}

function buildSbom({ modules, version, created, namespace }) {
  const packages = modules.map((m, i) => {
    const pkg = {
      SPDXID: `SPDXRef-Package-${i + 1}`,
      name: m.path,
      versionInfo: m.version,
      downloadLocation: "NOASSERTION",
      filesAnalyzed: false,
    }
    if (m.hash) pkg.comment = `go.sum ${m.hash}`
    pkg.externalRefs = [{
      referenceCategory: "PACKAGE-MANAGER",
      referenceType: "purl",
      referenceLocator: `pkg:golang/${m.path}@${m.version}`,
    }]
    return pkg
  })
  const relationships = [{
    spdxElementId: "SPDXRef-DOCUMENT",
    relationshipType: "DESCRIBES",
    relatedSpdxElement: "SPDXRef-Package-1",
  }]
  for (let i = 2; i <= modules.length; i++) {
    relationships.push({
      spdxElementId: "SPDXRef-Package-1",
      relationshipType: "DEPENDS_ON",
      relatedSpdxElement: `SPDXRef-Package-${i}`,
    })
  }
  const doc = {
    spdxVersion: "SPDX-2.3",
    dataLicense: "CC0-1.0",
    SPDXID: "SPDXRef-DOCUMENT",
    name: `lm-${version}`,
    documentNamespace: namespace,
    creationInfo: { created, creators: ["Tool: go-version-m"] },
    packages,
    relationships,
  }
  return `${JSON.stringify(doc, null, 2)}\n`
}

function releaseFiles(dir) {
  const binaries = fs.readdirSync(dir).filter((n) => n.startsWith("lm_")).sort()
  if (binaries.length === 0) {
    throw new Error(`no lm_* binaries in ${dir}`)
  }
  return [...binaries, ...EXTRA_FILES, SBOM_NAME]
}

function sha256sums(dir, names) {
  const lines = names.map((name) => {
    const hex = crypto.createHash("sha256").update(fs.readFileSync(path.join(dir, name))).digest("hex")
    return `${hex}  ${name}`
  })
  lines.sort()
  return `${lines.join("\n")}\n`
}

async function runResolveVersion({ context, core }) {
  core.setOutput("version", resolveVersion({
    eventName: context.eventName,
    refName: context.ref.replace(/^refs\/tags\//, ""),
    sha: context.sha,
  }))
}

async function runAssemble({ context, core, exec, version, dir = "dist" }) {
  for (const name of EXTRA_FILES) {
    fs.copyFileSync(name, path.join(dir, name))
  }
  const epoch = (await exec.getExecOutput("git", ["log", "-1", "--format=%ct"])).stdout.trim()
  const goVersionM = (await exec.getExecOutput("go", ["version", "-m", path.join(dir, "lm_linux_amd64")])).stdout
  const repo = `${context.repo.owner}/${context.repo.repo}`
  fs.writeFileSync(path.join(dir, SBOM_NAME), buildSbom({
    modules: parseModules(goVersionM, version),
    version,
    created: createdFromEpoch(epoch),
    namespace: `https://github.com/${repo}/releases/${version}/${SBOM_NAME}`,
  }))
  const sums = sha256sums(dir, releaseFiles(dir))
  fs.writeFileSync(path.join(dir, SUMS_NAME), sums)
  core.info(sums)
}

async function runAttach({ github, context, core, tag, dir = "dist" }) {
  const { owner, repo } = context.repo
  const releases = await github.paginate(github.rest.repos.listReleases, { owner, repo, per_page: 100 })
  let release = releases.find((r) => r.tag_name === tag)
  if (!release) {
    await github.rest.git.getRef({ owner, repo, ref: `tags/${tag}` })
    release = (await github.rest.repos.createRelease({
      owner, repo, tag_name: tag, name: tag, draft: true, generate_release_notes: true,
    })).data
  }
  const assets = release.assets || []
  for (const name of [...releaseFiles(dir), SUMS_NAME]) {
    const existing = assets.find((a) => a.name === name)
    if (existing) {
      await github.rest.repos.deleteReleaseAsset({ owner, repo, asset_id: existing.id })
    }
    const data = fs.readFileSync(path.join(dir, name))
    await github.rest.repos.uploadReleaseAsset({
      owner,
      repo,
      release_id: release.id,
      name,
      data,
      headers: { "content-type": "application/octet-stream", "content-length": data.length },
    })
    core.info(`uploaded ${name}`)
  }
}

module.exports = {
  resolveVersion,
  parseModules,
  createdFromEpoch,
  buildSbom,
  releaseFiles,
  sha256sums,
  runResolveVersion,
  runAssemble,
  runAttach,
}
