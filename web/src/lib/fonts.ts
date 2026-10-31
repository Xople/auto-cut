import { fontFileUrl } from '../api'
import type { Font } from '../types'

let styleEl: HTMLStyleElement | null = null
let signature = ''

export function isBoldFace(font: Font): boolean {
  return /bold|black|semibold|demibold|heavy|extrabold/i.test(font.subfamily)
}

export function applyBundledFonts(fonts: Font[]): void {
  const bundled = fonts.filter((font) => font.source === 'bundled' && font.id)
  if (!bundled.length) return
  const next = bundled.map((font) => `${font.id}:${isBoldFace(font) ? 700 : 400}`).join(',')
  if (next === signature) return
  signature = next

  if (!styleEl) {
    styleEl = document.createElement('style')
    styleEl.id = 'autocut-bundled-fonts'
    document.head.appendChild(styleEl)
  }

  styleEl.textContent = bundled
    .map((font) => {
      const weight = isBoldFace(font) ? 700 : 400
      const family = font.family.replace(/"/g, '')
      return `@font-face{font-family:"${family}";src:url("${fontFileUrl(font.id)}") format("truetype");font-weight:${weight};font-style:normal;font-display:swap;}`
    })
    .join('\n')
}
