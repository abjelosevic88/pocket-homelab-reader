// Finding a highlight again in an article's text, and marking it. Text is
// compared with whitespace collapsed, so a passage selected across
// paragraphs still matches; the words just before it (prefix) pick the right
// one when the same passage appears twice.

interface Mapped { norm: string; map: number[]; nodes: { node: Text; start: number }[] }

function textOf(root: HTMLElement): Mapped {
  const nodes: { node: Text; start: number }[] = []
  let full = ''
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode: n => (n.parentElement?.closest('script, style, .hl-skip') ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
  })
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    nodes.push({ node: n as Text, start: full.length })
    full += (n as Text).data
  }
  let norm = ''
  const map: number[] = []
  let space = true
  for (let i = 0; i < full.length; i++) {
    const c = full[i]
    if (/\s/.test(c)) {
      if (!space) { norm += ' '; map.push(i); space = true }
    } else {
      norm += c.toLowerCase(); map.push(i); space = false
    }
  }
  return { norm, map, nodes }
}

const squash = (s: string) => s.replace(/\s+/g, ' ').trim().toLowerCase()

/** Where a passage is in the article's text (original offsets), or null. */
export function locate(root: HTMLElement, text: string, prefix = ''): { start: number; end: number; m: Mapped } | null {
  const m = textOf(root)
  const needle = squash(text)
  if (!needle) return null
  const pre = squash(prefix).slice(-24)
  let best = -1
  for (let i = m.norm.indexOf(needle); i >= 0; i = m.norm.indexOf(needle, i + 1)) {
    if (best < 0) best = i
    if (pre && m.norm.slice(Math.max(0, i - pre.length - 1), i).trim().endsWith(pre)) { best = i; break }
  }
  if (best < 0) return null
  const start = m.map[best]
  const end = m.map[best + needle.length - 1] + 1
  return { start, end, m }
}

/** Wraps a located passage in <mark> elements (one per text node it spans). */
export function mark(root: HTMLElement, text: string, prefix: string, attrs: Record<string, string>): HTMLElement | null {
  const at = locate(root, text, prefix)
  if (!at) return null
  let first: HTMLElement | null = null
  for (const { node, start } of at.m.nodes) {
    const len = node.data.length
    const a = Math.max(at.start - start, 0)
    const b = Math.min(at.end - start, len)
    if (b <= 0 || a >= len || a >= b) continue
    let target = node
    if (a > 0) target = target.splitText(a)
    if (b - a < target.data.length) target.splitText(b - a)
    if (!target.data.trim()) continue
    const el = document.createElement('mark')
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v)
    target.parentNode!.insertBefore(el, target)
    el.appendChild(target)
    if (!first) first = el
  }
  return first
}

/** The selection inside root as a highlight to save: text, the words around it, where it is. */
export function fromSelection(root: HTMLElement): { text: string; prefix: string; suffix: string; position: number } | null {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return null
  const r = sel.getRangeAt(0)
  if (!root.contains(r.commonAncestorContainer)) return null
  const text = sel.toString().trim()
  if (text.length < 2) return null
  const before = document.createRange()
  before.selectNodeContents(root)
  before.setEnd(r.startContainer, r.startOffset)
  const after = document.createRange()
  after.selectNodeContents(root)
  after.setStart(r.endContainer, r.endOffset)
  const pre = before.toString()
  const total = (root.textContent || '').length || 1
  return { text, prefix: pre.slice(-64), suffix: after.toString().slice(0, 64), position: Math.min(1, pre.length / total) }
}
