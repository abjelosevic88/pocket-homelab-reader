// Every list: Unread, a feed, a folder, Read later, Starred, History, a tag,
// a search. On a wide screen the article opens beside the list; on a phone
// it opens on its own page.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { VIEW_EMPTY, VIEW_TITLE, announce, feedName, listQuery, openAdd, readTime, setState, since, type ListSpec, type View } from '../lib/reader'
import type { Article, Page } from '../lib/types'
import { Icon } from '../components/icons'
import { useCrumb, useFeeds, useOnChange } from '../components/Layout'
import { Reader } from '../components/Reader'
import { FeedIcon } from '../components/Sidebar'
import { Confirm, Empty, type MenuItem, MenuButton, Spinner, useMediaQuery, useToast } from '../components/ui'
import { CatchUp } from '../components/CatchUp'

export default function Stream({ view }: { view: View | 'feed' | 'folder' | 'tag' | 'search' }) {
  const params = useParams()
  const [sp, setSp] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { feeds, folders, counts, reload: reloadFeeds } = useFeeds()
  const { toast } = useToast()
  const wide = useMediaQuery('(min-width: 1100px)')
  const spec: ListSpec = useMemo(() => {
    switch (view) {
      case 'feed': return { view: 'all', feed: Number(params.id) }
      case 'folder': return { view: 'all', folder: Number(params.id) }
      case 'tag': return { view: 'all', tag: params.tag }
      case 'search': return { view: 'all', q: sp.get('q') || '' }
      default: return { view }
    }
  }, [view, params.id, params.tag, sp])
  const feed = spec.feed ? feeds.find(f => f.id === spec.feed) : undefined
  const folder = spec.folder ? folders.find(f => f.id === spec.folder) : undefined
  const title = feed ? feedName(feed) : folder ? folder.name : spec.tag ? `#${spec.tag}` : spec.q !== undefined ? `“${spec.q}”` : VIEW_TITLE[spec.view]
  useCrumb(view === 'feed' || view === 'folder' || view === 'tag' || view === 'search' ? title : undefined)

  const [items, setItems] = useState<Article[] | null>(null)
  const [next, setNext] = useState('')
  const [more, setMore] = useState(false)
  const [hideRead, setHideRead] = useState(false)
  const [confirmAll, setConfirmAll] = useState(false)
  const [catchUp, setCatchUp] = useState(false)
  const selected = Number(sp.get('a')) || 0
  const q = listQuery(spec) + (hideRead && (spec.feed || spec.folder) ? '&view=unread' : '')
  const gen = useRef(0)

  const load = useCallback(async () => {
    const g = ++gen.current
    try { const p = await api.get<Page>(`${V1}/articles?${q}&limit=40`); if (g === gen.current) { setItems(p.items); setNext(p.next) } } catch (e) { if (g === gen.current) { setItems([]); toast((e as Error).message, 'err') } }
  }, [q]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setItems(null); load() }, [load])
  useEffect(() => { setHideRead(!!settings?.hide_read) }, [settings?.hide_read, spec.feed, spec.folder])
  // a change elsewhere (an article starred in the reader) refreshes the rows in place, never reorders them
  useOnChange(() => {
    if (!items || items.length === 0) return
    api.get<Page>(`${V1}/articles?ids=${items.map(i => i.id).join(',')}&view=all&limit=${items.length}`).then(p => {
      const by = new Map(p.items.map(i => [i.id, i]))
      setItems(cur => cur ? cur.flatMap(i => by.has(i.id) ? [by.get(i.id)!] : spec.view === 'unread' || spec.view === 'saved' ? [i] : []) : cur)
    }).catch(() => {})
  })
  const loadMore = async () => {
    if (!next || more) return
    setMore(true)
    try { const p = await api.get<Page>(`${V1}/articles?${q}&limit=40&cursor=${encodeURIComponent(next)}`); setItems(x => [...(x || []), ...p.items.filter(i => !(x || []).some(y => y.id === i.id))]); setNext(p.next) } catch { /* keep the list */ } finally { setMore(false) }
  }
  const sentinel = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = sentinel.current
    if (!el || !next) return
    const io = new IntersectionObserver(es => { if (es.some(e => e.isIntersecting)) loadMore() }, { rootMargin: '600px' })
    io.observe(el)
    return () => io.disconnect()
  }) // eslint-disable-line react-hooks/exhaustive-deps

  // mark read when scrolled past (when chosen in Settings)
  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!settings?.mark_on_scroll || !items || spec.view === 'saved') return
    const root = wide ? listRef.current : (document.querySelector('.content') as HTMLElement)
    if (!root) return
    const pending = new Set<number>()
    let timer = 0
    const flush = () => {
      const ids = [...pending]; pending.clear()
      if (ids.length) api.post(`${V1}/articles/state`, { ids, read: true }).then(() => { setItems(x => x ? x.map(i => ids.includes(i.id) ? { ...i, read: true } : i) : x); reloadFeeds() }).catch(() => {})
    }
    const io = new IntersectionObserver(es => {
      for (const e of es) {
        if (!e.isIntersecting && e.boundingClientRect.bottom < (e.rootBounds?.top ?? 0)) {
          const id = Number((e.target as HTMLElement).dataset.id)
          const it = items.find(i => i.id === id)
          if (it && !it.read) pending.add(id)
        }
      }
      window.clearTimeout(timer); timer = window.setTimeout(flush, 800)
    }, { root: wide ? root : null, threshold: 0 })
    root.querySelectorAll<HTMLElement>('.art-row').forEach(el => io.observe(el))
    return () => { io.disconnect(); window.clearTimeout(timer); flush() }
  }, [items?.length, settings?.mark_on_scroll, wide]) // eslint-disable-line react-hooks/exhaustive-deps

  const open = (a: Article) => {
    if (wide) { const n = new URLSearchParams(sp); n.set('a', String(a.id)); setSp(n, { replace: !!selected }) } else navigate(`/a/${a.id}`)
    if (settings?.mark_on_open && !a.read) setItems(x => x ? x.map(i => i.id === a.id ? { ...i, read: true } : i) : x)
  }
  const closeReader = () => { const n = new URLSearchParams(sp); n.delete('a'); setSp(n, { replace: true }) }
  const quick = async (a: Article, p: Partial<Article>, msg?: string) => {
    setItems(x => x ? x.map(i => i.id === a.id ? { ...i, ...p } : i) : x)
    try { await setState(a.id, p); announce(); if (msg) toast(msg) } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const markAll = async () => {
    setConfirmAll(false)
    const body: Record<string, unknown> = { before: new Date().toISOString() }
    if (spec.feed) body.feed_id = spec.feed
    if (spec.folder) body.folder_id = spec.folder
    try {
      const r = await api.post<{ changed: number }>(`${V1}/articles/read-all`, body)
      toast(r.changed ? `${r.changed} marked as read` : 'Nothing unread'); announce()
      if (spec.view === 'unread') setItems([]); else setItems(x => x ? x.map(i => i.feed_id ? { ...i, read: true } : i) : x)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const refreshFeed = async () => {
    if (!feed) return
    try { const r = await api.post<{ new: number; error?: string }>(`${V1}/feeds/${feed.id}/refresh`); if (r.error) toast(r.error, 'err'); else toast(r.new ? `${r.new} new` : 'Nothing new'); load(); reloadFeeds() } catch (e) { toast((e as Error).message, 'err') }
  }

  // j / k / o and Shift+A
  const keys = useRef<(e: KeyboardEvent) => void>(() => {})
  keys.current = (e: KeyboardEvent) => {
    const t = e.target as HTMLElement
    if (e.metaKey || e.ctrlKey || e.altKey || t.closest('input, textarea, [contenteditable="true"]') || document.querySelector('.modal-backdrop, .menu, .popover, .palette-backdrop') || !items?.length) return
    const i = items.findIndex(x => x.id === selected)
    if (e.key === 'j' || e.key === 'k') {
      e.preventDefault()
      const n = e.key === 'j' ? Math.min(items.length - 1, i + 1) : Math.max(0, i - 1)
      if (wide) { open(items[n]); document.querySelector(`.art-row[data-id="${items[n].id}"]`)?.scrollIntoView({ block: 'nearest' }) } else navigate(`/a/${items[n].id}`)
      if (n >= items.length - 3) loadMore()
    } else if ((e.key === 'o' || e.key === 'Enter') && i >= 0 && !wide) { e.preventDefault(); navigate(`/a/${items[i].id}`) } else if (e.key === 'A' && e.shiftKey) { e.preventDefault(); if (settings?.confirm_all) setConfirmAll(true); else markAll() }
  }
  useEffect(() => { const h = (e: KeyboardEvent) => keys.current(e); window.addEventListener('keydown', h); return () => window.removeEventListener('keydown', h) }, [])

  const unreadHere = feed ? feed.unread : folder ? folder.unread : spec.view === 'unread' ? counts?.unread ?? 0 : items?.filter(i => !i.read && i.feed_id).length ?? 0
  const canMarkAll = !!(feed || folder || spec.view === 'unread' || spec.view === 'today' || spec.view === 'all') && !spec.q && !spec.tag
  const sub = feed ? <>{feed.error && feed.error_count >= 3 ? <span className="bad">{feed.error}</span> : <>{unreadHere} unread · checked {feed.fetched_at ? since(feed.fetched_at) + ' ago' : 'soon'}</>}</>
    : spec.view === 'saved' ? `${counts?.saved ?? 0} to read` : spec.view === 'unread' ? `${counts?.unread ?? 0} unread in ${counts?.feeds ?? 0} feeds` : spec.q !== undefined ? 'Search results' : undefined
  const [emptyT, emptyB] = feed ? ['Nothing here', hideRead ? 'No unread posts in this feed.' : 'This feed has no posts yet.'] : spec.q ? ['No results', 'Try fewer or other words.'] : VIEW_EMPTY[spec.view]
  const menu = (): MenuItem[] => [
    ...(feed || folder ? [{ label: hideRead ? 'Show read posts too' : 'Show only unread', icon: 'eye', onSelect: () => setHideRead(h => !h) }] : []),
    ...(feed ? [{ label: 'Check this feed now', icon: 'refresh', onSelect: refreshFeed }, { label: 'Feed settings', icon: 'settings', onSelect: () => navigate(`/feeds?edit=${feed.id}`) }] : []),
    ...(feed?.site_url ? [{ label: 'Open the website', icon: 'external', onSelect: () => window.open(feed.site_url, '_blank', 'noopener') }] : []),
    ...(folder ? [{ label: 'Manage feeds', icon: 'rss', onSelect: () => navigate('/feeds') }] : []),
    ...(spec.view === 'saved' ? [{ label: 'Archive', icon: 'archive', onSelect: () => navigate('/archive') }] : []),
    { label: 'Add a feed or page', icon: 'plus', onSelect: () => openAdd() },
  ]
  const split = wide && !!items && items.length > 0
  const style = settings?.list_style || 'cards'
  const list = (
    <div className={`stream-list ls-${style}`} ref={listRef}>
      <header className="stream-head">
        <div className="grow">
          <h1 className="stream-title">{feed && <FeedIcon feed={feed} size={18} />}{title}</h1>
          {sub && <p className="muted small">{sub}</p>}
        </div>
        <div className="stream-actions">
          <button type="button" className="btn ghost icon-only" title="Catch me up: a short briefing of what is unread (AI)" aria-label="Catch me up" onClick={() => setCatchUp(true)} disabled={!canMarkAll || unreadHere === 0}><Icon name="sparkles" size={17} /></button>
          <button type="button" className="btn ghost icon-only" title="Mark all as read (Shift+A)" aria-label="Mark all as read" onClick={() => settings?.confirm_all ? setConfirmAll(true) : markAll()} disabled={!canMarkAll || unreadHere === 0}><Icon name="check-all" size={17} /></button>
          <MenuButton label="More" align="right" width={230} items={menu}><Icon name="more" size={17} /></MenuButton>
        </div>
      </header>
      {catchUp && <CatchUp feedId={spec.feed} folderId={spec.folder} onClose={() => setCatchUp(false)} onOpen={id => open({ id } as Article)} />}
      {items === null ? <div className="art-skel">{[1, 2, 3, 4, 5, 6].map(i => <div key={i} className="art-skel-row"><div className="skeleton" style={{ width: '30%', height: 11 }} /><div className="skeleton" style={{ width: '85%', height: 16, marginTop: 8 }} /><div className="skeleton" style={{ width: '65%', height: 12, marginTop: 8 }} /></div>)}</div>
        : items.length === 0 ? <Empty title={emptyT} icon={spec.view === 'unread' ? 'check-circle' : spec.view === 'saved' ? 'bookmark' : 'inbox'}>{emptyB}{(spec.view === 'unread' || spec.view === 'saved') && feeds.length === 0 && <div className="mt"><button type="button" className="btn primary" onClick={() => openAdd()}><Icon name="plus" size={15} />Add your first site</button></div>}</Empty>
          : <div className="art-list" role="list">
            {items.map(a => <ArticleRow key={a.id} a={a} active={wide && a.id === selected} showImages={settings?.show_images !== false} style={style} onOpen={() => open(a)} onQuick={(p, msg) => quick(a, p, msg)} />)}
            <div ref={sentinel} className="art-more">{more ? <Spinner /> : next ? <button type="button" className="btn ghost sm" onClick={loadMore}>Show more</button> : items.length > 8 ? <span className="faint small">That is everything.</span> : null}</div>
          </div>}
    </div>
  )
  return (
    <div className={`stream ${split ? 'split' : ''}`}>
      {list}
      {split && <div className="stream-reader">
        {selected ? <Reader key={selected} id={selected} embedded onBack={closeReader} onGone={() => { closeReader(); load() }} />
          : <div className="reader-empty"><Icon name="book-open" size={28} className="faint" /><p className="muted">Choose an article to read it here.</p><p className="faint small">j and k move through the list.</p></div>}
      </div>}
      {confirmAll && <Confirm title="Mark all as read?" message={feed ? `Every unread post of ${feedName(feed)}.` : folder ? `Every unread post in ${folder.name}.` : 'Every unread post in every feed.'} confirmLabel="Mark all as read" danger={false} onCancel={() => setConfirmAll(false)} onConfirm={markAll} />}
    </div>
  )
}

function ArticleRow({ a, active, showImages, style, onOpen, onQuick }: { a: Article; active: boolean; showImages: boolean; style: string; onOpen: () => void; onQuick: (p: Partial<Article>, msg?: string) => void }) {
  const img = showImages && style !== 'compact' && a.image_url
  return (
    <div role="listitem" data-id={a.id} className={`art-row ${a.read && !a.saved ? 'read' : ''} ${active ? 'active' : ''} ${img ? 'with-img' : ''}`}
      onClick={e => { if ((e.target as HTMLElement).closest('button, a')) return; onOpen() }}>
      <div className="art-main">
        <div className="art-src">
          {a.feed_id ? <FeedIcon feed={{ icon_url: a.feed_icon, kind: a.url.includes('youtube.com') ? 'youtube' : 'web' }} size={13} /> : <Icon name="globe" size={13} className="muted" />}
          <span className="ellipsis">{a.site_name || a.feed_title}</span>
          <span className="faint">· {since(a.saved && !a.feed_id ? a.saved_at : a.published_at)}</span>
          {!a.read && a.feed_id && <span className="unread-dot" aria-label="Unread" />}
        </div>
        <a className="art-title" href={`/a/${a.id}`} onClick={e => { e.preventDefault(); onOpen() }}>{a.title || 'Untitled'}</a>
        {style !== 'compact' && a.excerpt && <p className="art-excerpt">{a.excerpt}</p>}
        <div className="art-meta">
          <span className="faint">{readTime(a.words)}</span>
          {a.highlights > 0 && <span className="faint row" style={{ gap: 3 }}><Icon name="highlighter" size={12} />{a.highlights}</span>}
          {a.tags && <span className="faint ellipsis">{a.tags.split(',').slice(0, 3).map(t => '#' + t.trim()).join(' ')}</span>}
          {a.saved && a.progress > 0.02 && a.progress < 0.97 && <span className="art-progress" title={`${Math.round(a.progress * 100)}% read`}><span style={{ width: `${a.progress * 100}%` }} /></span>}
          <span className="grow" />
          <span className="art-quick">
            <button type="button" className={`btn ghost icon-only xs ${a.starred ? 'on-star' : ''}`} title={a.starred ? 'Unstar' : 'Star'} aria-label="Star" onClick={() => onQuick({ starred: !a.starred })}><Icon name="star" size={14} /></button>
            <button type="button" className={`btn ghost icon-only xs ${a.saved && !a.archived ? 'on-accent' : ''}`} title={a.saved ? 'Off the read-later list' : 'Read later'} aria-label="Read later" onClick={() => onQuick({ saved: !a.saved }, a.saved ? 'Off your read-later list' : 'Saved to read later')}><Icon name={a.saved ? 'bookmark-check' : 'bookmark'} size={14} /></button>
            {a.feed_id ? <button type="button" className="btn ghost icon-only xs" title={a.read ? 'Mark as unread' : 'Mark as read'} aria-label={a.read ? 'Mark as unread' : 'Mark as read'} onClick={() => onQuick({ read: !a.read })}><Icon name={a.read ? 'circle' : 'check'} size={14} /></button>
              : a.saved && <button type="button" className="btn ghost icon-only xs" title={a.archived ? 'Back to read later' : 'Done'} aria-label="Done" onClick={() => onQuick({ archived: !a.archived }, a.archived ? 'Back on your list' : 'Done — archived')}><Icon name="archive" size={14} /></button>}
          </span>
        </div>
      </div>
      {img && <img className="art-img" src={a.image_url} alt="" loading="lazy" referrerPolicy="no-referrer" onError={e => ((e.target as HTMLElement).style.visibility = 'hidden')} />}
    </div>
  )
}
