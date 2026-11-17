#!/usr/bin/env node
/**
 * Regenerate market/index.json for the plugin market from the package manifest,
 * the locale metadata and the icon. `PUBLISH_TIMESTAMP` (epoch seconds) pins the
 * publish time; without it the current time is used.
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const pluginRoot = path.resolve(here, '..')

function readJson(relative) {
  return JSON.parse(fs.readFileSync(path.join(pluginRoot, relative), 'utf8'))
}

function main() {
  const pkg = readJson('package.json')
  const en = readJson('locale/en.json')
  const zh = readJson('locale/zh.json')
  const icon = fs.readFileSync(path.join(pluginRoot, 'icon.svg'), 'utf8').trim()
  const iconData = `data:image/svg+xml;base64,${Buffer.from(icon, 'utf8').toString('base64')}`
  const publishTimestamp = Number(process.env.PUBLISH_TIMESTAMP || Math.floor(Date.now() / 1000))

  const index = {
    format: 1,
    title: { en: 'TwinsEarth Plugins', zh: 'TwinsEarth 插件' },
    description: {
      en: 'Plugins published by TwinsEarth, including the AI Prison governance bundle.',
      zh: 'TwinsEarth 发布的插件，包含 AI Prison 治理插件。',
    },
    plugins: [
      {
        name: pkg.name,
        title: { en: 'AI Prison', zh: 'AI Prison · AI 监狱' },
        description: { en: en.meta.description, zh: zh.meta.description },
        author: 'TwinsEarth',
        homepage: pkg.homepage,
        repository: { url: 'https://github.com/TwinsEarth/Ai-PRISON', directory: 'dsh-plugin' },
        icon: iconData,
        versions: [
          {
            version: pkg.version,
            publishTimestamp,
            source: { type: 'npm', registry: 'https://registry.npmjs.org' },
            engines: { dsh: pkg.engines.dsh },
          },
        ],
      },
    ],
  }

  const target = path.join(pluginRoot, 'market/index.json')
  fs.mkdirSync(path.dirname(target), { recursive: true })
  fs.writeFileSync(target, `${JSON.stringify(index, null, 2)}\n`)
  console.log(`wrote market/index.json for ${pkg.name}@${pkg.version} (publishTimestamp=${publishTimestamp})`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
