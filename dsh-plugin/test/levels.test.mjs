import assert from 'node:assert/strict'
import test from 'node:test'
import { parseLevels, summarizeLevels } from '../lib/levels.js'

const FIXTURE = `L0 Warning            [immediate] immediate, appealable
    - reputation deduction
    - record flag
L1 Restriction        [hours] hours
    - network whitelist narrowed
    - tool-call scope reduced
L2 Credential Freeze  [days] days
    - global credential revocation
    - channels cut
    - compute quota zeroed
`

test('parses every level with its deprivations', () => {
  const levels = parseLevels(FIXTURE)
  assert.equal(levels.length, 3)
  assert.deepEqual(levels[0], {
    code: 'L0',
    name: 'Warning',
    durationClass: 'immediate',
    durationHint: 'immediate, appealable',
    deprivations: ['reputation deduction', 'record flag'],
  })
  assert.equal(levels[1].deprivations.length, 2)
  assert.equal(levels[2].code, 'L2')
  assert.equal(levels[2].name, 'Credential Freeze')
  assert.equal(levels[2].deprivations.length, 3)
})

test('ignores noise lines and keeps the matrix when the CLI walks L0-L5', () => {
  const levels = parseLevels(`L0 Warning            [immediate] immediate, appealable
    - reputation deduction
  不是矩阵的行
L1 Restriction        [hours] hours
    - network whitelist narrowed
`)
  assert.equal(levels.length, 2)
  assert.equal(levels[0].deprivations.length, 1)
})

test('empty input yields an empty matrix', () => {
  assert.deepEqual(parseLevels(''), [])
  assert.deepEqual(parseLevels(undefined), [])
})

test('summarizeLevels keeps one readable line per level', () => {
  const lines = summarizeLevels(parseLevels(FIXTURE))
  assert.equal(lines.length, 3)
  assert.match(lines[0], /L0 Warning/)
})
