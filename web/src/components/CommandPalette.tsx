// ⌘K / "/" — search everything you read, jump to a feed, or run a command from the keyboard.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { feedName, since } from '../lib/reader'
import type { Article, Feed, Page } from '../lib/types'
import { Icon } from './icons'
import { useDebounce } from './ui'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

export function CommandPalette({ onClose, commands, feeds }: { onClose: () => void; commands: PaletteCommand[]; feeds: Feed[] }) {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [hi, setHi] = useState(0)
  const [found, setFound] = useState<Article[]>([])
  const [searching, setSearching] = useState(false)
  const list = useRef<HTMLDivElement>(null)
  const dq = useDebounce(q.trim(), 220)

  useEffect(() => {
    if (dq.length < 2) { setFound([]); return }
    let stop = false
    setSearching(true)
    api.get<Page>(`${V1}/articles?q=${encodeURIComponent(dq)}&limit=12`).then(p => { if (!stop) setFound(p.items) }).catch(() => {}).finally(() => { if (!stop) setSearching(false) })
    return () => { stop = true }
  }, [dq])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    const n = q.trim().toLowerCase()
    const words = n.split(/\s+/).filter(Boolean)
    const hit = (text: string) => words.every(w => text.toLowerCase().includes(w))
    const cmds = commands.filter(c => !n || hit(`${c.label} ${c.keywords || ''}`)).map(c => ({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint, run: c.run }))
    const fs = feeds.filter(f => n && hit(`${feedName(f)} ${f.url} ${f.site_url}`)).slice(0, 6)
      .map(f => ({ key: `f${f.id}`, group: 'Feeds', icon: <Icon name={f.kind === 'youtube' ? 'youtube' : 'rss'} size={16} />, label: feedName(f), sub: f.unread ? `${f.unread} unread` : undefined, run: () => navigate(`/feed/${f.id}`) }))
    const arts = found.map(a => ({ key: `a${a.id}`, group: 'Articles', icon: <Icon name={a.saved ? 'bookmark' : a.starred ? 'star' : 'text'} size={16} />, label: a.title,
      sub: `${a.site_name || a.feed_title} · ${since(a.published_at)}`, run: () => navigate(`/a/${a.id}`) }))
    if (n) {
      out.push(...arts, ...fs, ...cmds)
      out.push({ key: 'all', group: 'Search', icon: <Icon name="search" size={16} />, label: <>Search everything for “{q.trim()}”</>, run: () => navigate(`/search?q=${encodeURIComponent(q.trim())}`) })
    } else out.push(...cmds)
    return out
  }, [q, found, feeds, commands, navigate])
  useEffect(() => setHi(0), [q])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search what you read, a feed, or a command…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {rows.length === 0 && <div className="pal-empty">{searching ? 'Searching…' : 'Nothing matches.'}</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label">{r.label}</span>{r.sub && <span className="pal-sub">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
