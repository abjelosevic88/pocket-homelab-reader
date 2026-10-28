// Left column: add button, the lists with their numbers, folders with their feeds — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { feedName, groupFeeds } from '../lib/reader'
import type { Feed } from '../lib/types'
import { Icon } from './icons'
import { useFeeds } from './Layout'

const OPEN_KEY = 'prd-folders-closed'

export function FeedIcon({ feed, size = 16 }: { feed: Pick<Feed, 'icon_url' | 'kind'>; size?: number }) {
  const [broken, setBroken] = useState(false)
  if (feed.kind === 'youtube') return <Icon name="youtube" size={size} className="feed-ico yt" />
  if (!feed.icon_url || broken) return <Icon name="rss" size={size} className="feed-ico" />
  return <img src={feed.icon_url} alt="" width={size} height={size} className="feed-ico img" loading="lazy" referrerPolicy="no-referrer" onError={() => setBroken(true)} />
}

export function Sidebar({ onAdd, onRefresh, onNavigate }: { onAdd: () => void; onRefresh: () => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const { feeds, folders, counts } = useFeeds()
  const title = settings?.title || status?.title || 'Pocket Reader'
  const [closed, setClosed] = useState<string[]>(() => { try { return JSON.parse(localStorage.getItem(OPEN_KEY) || '[]') } catch { return [] } })
  const toggle = (id: string) => setClosed(c => { const n = c.includes(id) ? c.filter(x => x !== id) : [...c, id]; localStorage.setItem(OPEN_KEY, JSON.stringify(n)); return n })
  const n = (v?: number) => v ? (v > 999 ? '999+' : String(v)) : undefined
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string }[] = [
    { to: '/', label: 'Unread', icon: 'inbox', end: true, count: n(counts?.unread) },
    { to: '/saved', label: 'Read later', icon: 'bookmark', count: n(counts?.saved) },
    { to: '/starred', label: 'Starred', icon: 'star', count: n(counts?.starred) },
    { to: '/highlights', label: 'Highlights', icon: 'highlighter', count: n(counts?.highlights) },
    { to: '/history', label: 'History', icon: 'clock' },
  ]
  const { groups, loose } = groupFeeds(feeds, folders)
  const row = (f: Feed) => (
    <NavLink key={f.id} to={`/feed/${f.id}`} onClick={onNavigate} className={({ isActive }) => `sb-nav-item sb-feed ${isActive ? 'active' : ''} ${f.paused ? 'paused' : ''}`} title={f.error ? `${feedName(f)}: ${f.error}` : feedName(f)}>
      <FeedIcon feed={f} size={15} /><span className="ellipsis grow">{feedName(f)}</span>
      {f.error && f.error_count >= 3 ? <Icon name="alert" size={13} className="sb-warn" /> : !!f.unread && <span className="sb-count">{n(f.unread)}</span>}
    </NavLink>
  )
  return (
    <aside className="sidebar" aria-label="Reader">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Reader' ? title : <>Pocket <b>Reader</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onAdd} title="Follow a website or save a page (A)">
        <Icon name="plus" size={15} strokeWidth={2.2} />Add
      </button>

      <nav className="sb-nav" aria-label="Lists">
        {nav.map(x => (
          <NavLink key={x.to} to={x.to} end={x.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={x.icon} size={16} />{x.label}
            {!!x.count && <span className="sb-count">{x.count}</span>}
          </NavLink>
        ))}
      </nav>

      <section className="sb-sec sb-feeds" aria-label="Feeds">
        <h4 className="sb-h">
          <Link to="/feeds" className="sb-h-btn" onClick={onNavigate}>Feeds</Link>
          <span className="row" style={{ gap: 2 }}>
            <button type="button" className="btn ghost icon-only xs" onClick={onRefresh} title="Check all feeds now (R)" aria-label="Check all feeds now"><Icon name="refresh" size={14} /></button>
            <button type="button" className="btn ghost icon-only xs" onClick={onAdd} title="Follow a website" aria-label="Follow a website"><Icon name="plus" size={14} /></button>
          </span>
        </h4>
        {feeds.length === 0 && <p className="sb-empty muted small">No feeds yet. Paste any website — a blog, a newspaper, a YouTube channel — and Reader finds its feed.</p>}
        {groups.filter(g => g.feeds.length > 0).map(g => {
          const id = `f${g.folder.id}`
          const open = !closed.includes(id)
          return (
            <div key={g.folder.id} className="sb-folder">
              <div className="sb-folder-row">
                <button type="button" className="sb-fold" onClick={() => toggle(id)} aria-expanded={open} aria-label={open ? `Close ${g.folder.name}` : `Open ${g.folder.name}`}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} /></button>
                <NavLink to={`/folder/${g.folder.id}`} onClick={onNavigate} className={({ isActive }) => `sb-nav-item sb-folder-link ${isActive ? 'active' : ''}`}>
                  <Icon name="folder" size={15} /><span className="ellipsis grow">{g.folder.name}</span>{!!g.folder.unread && <span className="sb-count">{n(g.folder.unread)}</span>}
                </NavLink>
              </div>
              {open && <div className="sb-folder-feeds">{g.feeds.map(row)}</div>}
            </div>
          )
        })}
        {loose.map(row)}
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('rd:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
