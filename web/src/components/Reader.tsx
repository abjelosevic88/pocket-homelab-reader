// The article: clean text in the person's typography, with stars, read
// later, highlights (select text → Highlight), a note, the AI summary and
// "send highlights to Pocket Notes". Used beside the list and on its own page.
import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fromSelection, mark } from '../lib/highlight'
import { announce, feedName, hostOf, setState, since, splitTags } from '../lib/reader'
import type { ArticleView, Highlight, Settings } from '../lib/types'
import { Icon } from './icons'
import { FeedIcon } from './Sidebar'
import { type Anchor, Confirm, CoreHint, type MenuItem, MenuButton, Popover, Segmented, Spinner, Switch, copyText, rectOf, useIsMobile, useToast } from './ui'

const COLORS: Highlight['color'][] = ['yellow', 'green', 'blue', 'pink']

export function Reader({ id, onBack, onGone, embedded }: { id: number; onBack?: () => void; onGone?: () => void; embedded?: boolean }) {
  const navigate = useNavigate()
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const [a, setA] = useState<ArticleView | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState('')
  const [summary, setSummary] = useState<{ text: string; error?: string } | null>(null)
  const [sel, setSel] = useState<{ x: number; y: number } | null>(null)
  const [aa, setAa] = useState<Anchor | null>(null)
  const [del, setDel] = useState(false)
  const [editTags, setEditTags] = useState<string | null>(null)
  const body = useRef<HTMLDivElement>(null)
  const scroller = useRef<HTMLElement | null>(null)
  const progress = useRef(0)
  const saved = useRef(0)

  const load = useCallback(async () => {
    try {
      const v = await api.get<ArticleView>(`${V1}/articles/${id}`)
      setA(v); setErr('')
      setSummary(v.ai_summary ? { text: v.ai_summary } : null)
      progress.current = saved.current = v.progress
      if (settings?.mark_on_open && !v.read) {
        setState(id, { read: true }).then(() => announce()).catch(() => {})
        setA(x => x ? { ...x, read: true } : x)
      }
    } catch (e) { setErr((e as Error).message) }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setA(null); setSummary(null); load() }, [load])

  // find the scrolling box (the reader pane or the page) and keep the reading position
  useLayoutEffect(() => {
    if (!a || !body.current) return
    let el: HTMLElement | null = body.current.parentElement
    while (el && !(el.scrollHeight > el.clientHeight && /(auto|scroll)/.test(getComputedStyle(el).overflowY))) el = el.parentElement
    scroller.current = el || (document.querySelector('.content') as HTMLElement)
    const sc = scroller.current
    if (sc && a.progress > 0.05 && a.progress < 0.97) sc.scrollTop = a.progress * (sc.scrollHeight - sc.clientHeight)
    else if (sc) sc.scrollTop = 0
  }, [a?.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const sc = scroller.current
    if (!a || !sc) return
    const on = () => { const max = sc.scrollHeight - sc.clientHeight; progress.current = max > 0 ? Math.min(1, sc.scrollTop / max) : 1 }
    sc.addEventListener('scroll', on, { passive: true })
    const t = window.setInterval(() => {
      if (Math.abs(progress.current - saved.current) > 0.04 || (progress.current > 0.97 && saved.current < 0.97)) { saved.current = progress.current; setState(a.id, { progress: Math.round(progress.current * 100) / 100 }).catch(() => {}) }
    }, 3000)
    return () => {
      sc.removeEventListener('scroll', on); window.clearInterval(t)
      if (Math.abs(progress.current - saved.current) > 0.02) setState(a.id, { progress: Math.round(progress.current * 100) / 100 }).catch(() => {})
    }
  }, [a?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  // mark the highlights in the text
  useLayoutEffect(() => {
    const root = body.current
    if (!root || !a) return
    root.querySelectorAll('mark.hl').forEach(m => { const p = m.parentNode!; while (m.firstChild) p.insertBefore(m.firstChild, m); p.removeChild(m) })
    root.normalize()
    for (const h of a.highlight_list || []) mark(root, h.text, h.prefix, { class: `hl hl-${h.color}`, 'data-hl': String(h.id), title: h.note || 'Highlight' })
  }, [a?.content, a?.highlight_list]) // eslint-disable-line react-hooks/exhaustive-deps

  // the selection toolbar
  useEffect(() => {
    const h = () => {
      const root = body.current
      const s = window.getSelection()
      if (!root || !s || s.isCollapsed || s.rangeCount === 0 || !root.contains(s.getRangeAt(0).commonAncestorContainer) || s.toString().trim().length < 2) { setSel(null); return }
      const r = s.getRangeAt(0).getBoundingClientRect()
      setSel({ x: r.left + r.width / 2, y: r.top })
    }
    document.addEventListener('selectionchange', h)
    return () => document.removeEventListener('selectionchange', h)
  }, [])

  const patch = async (p: Parameters<typeof setState>[1], msg?: string) => {
    if (!a) return
    setA({ ...a, ...p })
    try { await setState(a.id, p); announce(); if (msg) toast(msg) } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const highlight = async (color: Highlight['color'] = 'yellow') => {
    const root = body.current
    if (!root || !a) return
    const h = fromSelection(root)
    if (!h) return
    window.getSelection()?.removeAllRanges(); setSel(null)
    try {
      const made = await api.post<Highlight>(`${V1}/articles/${a.id}/highlights`, { ...h, color })
      setA(x => x ? { ...x, highlight_list: [...(x.highlight_list || []), made].sort((p, q) => p.position - q.position), highlights: x.highlights + 1 } : x)
      announce()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const updateHl = async (h: Highlight, p: Partial<Highlight>) => {
    try { const got = await api.put<Highlight>(`${V1}/highlights/${h.id}`, p); setA(x => x ? { ...x, highlight_list: x.highlight_list.map(y => y.id === h.id ? got : y) } : x) } catch (e) { toast((e as Error).message, 'err') }
  }
  const removeHl = async (h: Highlight) => {
    try { await api.del(`${V1}/highlights/${h.id}`); setA(x => x ? { ...x, highlight_list: x.highlight_list.filter(y => y.id !== h.id), highlights: x.highlights - 1 } : x); announce() } catch (e) { toast((e as Error).message, 'err') }
  }
  const summarize = async (fresh = false) => {
    if (!a) return
    setBusy('summary')
    try {
      const r = await api.post<{ ok: boolean; text?: string; error?: string }>(`${V1}/articles/${a.id}/summary${fresh ? '?fresh=1' : ''}`)
      setSummary(r.ok ? { text: r.text || '' } : { text: '', error: r.error })
    } catch (e) { setSummary({ text: '', error: (e as Error).message }) } finally { setBusy('') }
  }
  const extract = async () => {
    if (!a) return
    setBusy('extract')
    try { const v = await api.post<ArticleView>(`${V1}/articles/${a.id}/extract`); setA(x => x ? { ...x, ...v, highlight_list: x.highlight_list } : x); if (v.extract_state !== 'done') toast(v.extract_error || 'No readable article on that page', 'err') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const toNotes = async () => {
    if (!a) return
    setBusy('notes')
    try {
      const r = await api.post<{ ok: boolean; error?: string; note_id?: number }>(`${V1}/articles/${a.id}/notes`)
      if (r.ok) { toast('Highlights are in Pocket Notes'); setA(x => x ? { ...x, notes_note_id: r.note_id || x.notes_note_id, notes_synced_at: new Date().toISOString() } : x) } else toast(r.error || 'failed', 'err')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const remove = async () => {
    if (!a) return
    setDel(false)
    try { await api.del(`${V1}/articles/${a.id}`); announce(); toast('Removed'); onGone ? onGone() : navigate(-1) } catch (e) { toast((e as Error).message, 'err') }
  }
  const saveTags = async () => {
    if (editTags === null || !a) return
    const t = editTags; setEditTags(null)
    await patch({ tags: splitTags(t).join(', ') })
  }

  // article keys
  const keysRef = useRef<(e: KeyboardEvent) => void>(() => {})
  keysRef.current = (e: KeyboardEvent) => {
    if (!a) return
    const t = e.target as HTMLElement
    if (e.metaKey || e.ctrlKey || e.altKey || t.closest('input, textarea, [contenteditable="true"]') || document.querySelector('.modal-backdrop, .menu, .popover, .palette-backdrop')) return
    switch (e.key) {
      case 's': e.preventDefault(); patch({ starred: !a.starred }); break
      case 'l': e.preventDefault(); patch({ saved: !a.saved }, a.saved ? 'Off your read-later list' : 'Saved to read later'); break
      case 'e': if (a.saved) { e.preventDefault(); patch({ archived: !a.archived }, a.archived ? 'Back on your list' : 'Done — moved to the archive') } break
      case 'm': e.preventDefault(); patch({ read: !a.read }); break
      case 'v': if (a.url) { e.preventDefault(); window.open(a.url, '_blank', 'noopener') } break
      case 'h': if (sel) { e.preventDefault(); highlight() } break
      case 'Escape': if (onBack && !sel) { e.preventDefault(); onBack() } break
    }
  }
  useEffect(() => { const h = (e: KeyboardEvent) => keysRef.current(e); window.addEventListener('keydown', h); return () => window.removeEventListener('keydown', h) }, [])

  if (err) return <div className="reader-empty"><div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div></div>
  if (!a) return <div className="reader-skel" aria-busy="true"><div className="skeleton" style={{ width: '40%', height: 14 }} /><div className="skeleton" style={{ width: '90%', height: 30, marginTop: 14 }} /><div className="skeleton" style={{ width: '70%', height: 30, marginTop: 8 }} />{[1, 2, 3, 4, 5].map(i => <div key={i} className="skeleton" style={{ width: `${96 - i * 4}%`, height: 14, marginTop: i === 1 ? 32 : 12 }} />)}</div>

  const st = settings
  const site = a.feed ? feedName(a.feed) : (a.site_name || hostOf(a.url))
  const thin = (a.content || '').replace(/<[^>]+>/g, '').trim().length < 600
  const menu = (): MenuItem[] => [
    { label: a.read ? 'Mark as unread' : 'Mark as read', icon: 'check', hint: 'M', onSelect: () => patch({ read: !a.read }) },
    ...(a.saved ? [{ label: a.archived ? 'Back to read later' : 'Done — archive it', icon: 'archive', hint: 'E', onSelect: () => patch({ archived: !a.archived }) }] : []),
    { label: 'Fetch the whole article', icon: 'download', onSelect: extract, disabled: !a.url },
    { label: 'Summarise with AI', icon: 'sparkles', onSelect: () => summarize(!!summary?.text) },
    { label: 'Send highlights to Pocket Notes', icon: 'note', onSelect: toNotes, disabled: !a.highlight_list?.length && !a.notes_note_id },
    { label: 'Edit tags', icon: 'tag', onSelect: () => setEditTags(a.tags) },
    { label: 'Copy link', icon: 'link', onSelect: () => { copyText(a.url).then(ok => toast(ok ? 'Link copied' : 'Could not copy', ok ? 'ok' : 'err')) }, disabled: !a.url },
    ...(navigator.share ? [{ label: 'Share…', icon: 'share', onSelect: () => { navigator.share({ title: a.title, url: a.url }).catch(() => {}) } }] : []),
    'sep',
    { label: a.feed_id ? 'Remove this post' : 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) },
  ]
  const width = st?.line_width || 'medium'
  return (
    <article className={`reader rt-${st?.reader_theme || 'app'} ${embedded ? 'embedded' : ''}`} lang={a.lang || undefined}>
      <div className="reader-bar">
        {onBack && <button type="button" className="btn ghost icon-only" onClick={onBack} aria-label="Back" title="Back (esc)"><Icon name="chevron-left" size={18} /></button>}
        <span className="grow" />
        <button type="button" className={`btn ghost icon-only ${a.starred ? 'on-star' : ''}`} onClick={() => patch({ starred: !a.starred })} aria-pressed={a.starred} title={a.starred ? 'Starred (S)' : 'Star (S)'} aria-label="Star"><Icon name="star" size={17} /></button>
        <button type="button" className={`btn ghost icon-only ${a.saved && !a.archived ? 'on-accent' : ''}`} onClick={() => patch({ saved: !a.saved }, a.saved ? 'Off your read-later list' : 'Saved to read later')} aria-pressed={a.saved} title={a.saved ? 'On your read-later list (L)' : 'Read later (L)'} aria-label="Read later"><Icon name={a.saved ? 'bookmark-check' : 'bookmark'} size={17} /></button>
        <button type="button" className="btn ghost icon-only" onClick={() => patch({ read: !a.read })} title={a.read ? 'Mark as unread (M)' : 'Mark as read (M)'} aria-label={a.read ? 'Mark as unread' : 'Mark as read'}><Icon name={a.read ? 'circle' : 'check-circle'} size={17} /></button>
        <button type="button" className="btn ghost icon-only" onClick={() => summarize(false)} disabled={busy === 'summary'} title="Summarise with AI" aria-label="Summarise with AI">{busy === 'summary' ? <Spinner /> : <Icon name="sparkles" size={17} />}</button>
        <button type="button" className="btn ghost icon-only" onClick={e => setAa(aa ? null : rectOf(e.currentTarget))} title="Text size and style" aria-label="Text size and style"><Icon name="type" size={17} /></button>
        <a className="btn ghost icon-only" href={a.url || undefined} target="_blank" rel="noopener noreferrer" title="Open the original (V)" aria-label="Open the original" aria-disabled={!a.url}><Icon name="external" size={17} /></a>
        <MenuButton label="More" align="right" width={250} items={menu}><Icon name="more" size={17} /></MenuButton>
      </div>
      <div className={`reader-page w-${width}`}>
        <header className="reader-head">
          <div className="reader-site">
            {a.feed ? <FeedIcon feed={a.feed} size={15} /> : <Icon name="globe" size={15} className="muted" />}
            {a.feed ? <button type="button" className="link-btn" onClick={() => navigate(`/feed/${a.feed!.id}`)}>{site}</button> : <span>{site}</span>}
            <span className="muted">· {since(a.published_at || a.saved_at)}</span>
          </div>
          <h1 className="reader-title">{a.url ? <a href={a.url} target="_blank" rel="noopener noreferrer">{a.title || 'Untitled'}</a> : a.title || 'Untitled'}</h1>
          <div className="reader-meta muted small">
            {a.author && <span>{a.author}</span>}
            <span>{a.minutes} min read</span>
            {a.saved && <span className="badge accent">{a.archived ? 'Archived' : 'Read later'}</span>}
            {splitTags(a.tags).map(t => <button key={t} type="button" className="chip tag-chip" onClick={() => navigate(`/tag/${encodeURIComponent(t)}`)}>#{t}</button>)}
            <button type="button" className="link-btn small" onClick={() => setEditTags(a.tags)}><Icon name="tag" size={12} />{a.tags ? 'Edit' : 'Add tags'}</button>
          </div>
          {editTags !== null && <form className="row mt-s" onSubmit={e => { e.preventDefault(); saveTags() }}>
            <input autoFocus value={editTags} onChange={e => setEditTags(e.target.value)} placeholder="reading, cooking, work" aria-label="Tags" onKeyDown={e => { if (e.key === 'Escape') { e.stopPropagation(); setEditTags(null) } }} />
            <button className="btn sm primary">Save</button><button type="button" className="btn sm ghost" onClick={() => setEditTags(null)}>Cancel</button>
          </form>}
        </header>

        {summary && <div className={`reader-summary ${summary.error ? 'bad' : ''}`}>
          <div className="row"><Icon name="sparkles" size={15} /><b className="grow">Summary</b>{!summary.error && <button type="button" className="btn ghost xs" onClick={() => summarize(true)} disabled={busy === 'summary'}>Again</button>}<button type="button" className="btn ghost icon-only xs" aria-label="Close" onClick={() => setSummary(null)}><Icon name="x" size={13} /></button></div>
          {summary.error ? <p className="small">{summary.error}{/Pocket Core|Pocket AI/.test(summary.error) && <CoreHint />}</p> : <div className="summary-text">{summary.text.split('\n').filter(Boolean).map((l, i) => <p key={i}>{l.replace(/^[-•]\s*/, '')}</p>)}</div>}
        </div>}

        {a.media_url && a.media_type.startsWith('audio/') && <audio className="reader-audio" controls preload="none" src={a.media_url} />}
        {a.media_url && a.media_type.startsWith('video/') && <video className="reader-video" controls preload="none" src={a.media_url} />}

        {(a.extract_state === 'pending' || a.extract_state === 'failed' || (thin && a.url && a.feed?.kind !== 'youtube')) && <div className="callout info reader-callout">
          <Icon name="info" size={15} />
          <div className="grow">{a.extract_state === 'pending' ? 'Fetching the whole article…' : a.extract_state === 'failed' ? (a.extract_error || 'The page has no readable article.') : 'The feed sent only the start of this post.'}</div>
          <button type="button" className="btn sm" onClick={extract} disabled={busy === 'extract'}>{busy === 'extract' ? <Spinner /> : 'Fetch whole article'}</button>
        </div>}

        <div ref={body} className={`reader-content f-${st?.font || 'serif'} lh-${st?.line_height || 'normal'} ${st?.justify ? 'justify' : ''}`} style={{ fontSize: st?.font_size || 19 }}
          onClick={e => { const m = (e.target as HTMLElement).closest('mark.hl'); if (m) document.getElementById(`hl-${m.getAttribute('data-hl')}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }) }}
          dangerouslySetInnerHTML={{ __html: a.content || `<p>${a.excerpt || ''}</p>` }} />

        <footer className="reader-foot">
          {a.url && <a className="btn" href={a.url} target="_blank" rel="noopener noreferrer"><Icon name="external" size={15} />Open on {hostOf(a.url)}</a>}
          {a.saved && !a.archived && <button type="button" className="btn primary" onClick={() => patch({ archived: true, progress: 1 }, 'Done — moved to the archive')}><Icon name="check" size={15} />Done</button>}
          {!a.saved && <button type="button" className="btn" onClick={() => patch({ saved: true }, 'Saved to read later')}><Icon name="bookmark" size={15} />Read later</button>}
        </footer>

        <Highlights a={a} st={st} busy={busy === 'notes'} onNotes={toNotes} onUpdate={updateHl} onRemove={removeHl} />
        <NoteBox key={a.id} note={a.note} onSave={note => patch({ note })} />
      </div>

      {sel && <SelectionBar x={sel.x} y={sel.y} mobile={mobile} onPick={highlight} />}
      {aa && st && <Popover anchor={aa} onClose={() => setAa(null)} width={300} prefer="below" label="Text">
        <TextStyle st={st} onChange={async p => { try { setSettings(await api.put<Settings>(`${V1}/settings`, p)) } catch (e) { toast((e as Error).message, 'err') } }} />
      </Popover>}
      {del && <Confirm title="Remove this article?" message={a.highlight_list?.length ? 'Its highlights and your note go with it.' : 'It disappears from every list.'} confirmLabel="Remove" onCancel={() => setDel(false)} onConfirm={remove} />}
    </article>
  )
}

function SelectionBar({ x, y, mobile, onPick }: { x: number; y: number; mobile: boolean; onPick: (c: Highlight['color']) => void }) {
  const style: React.CSSProperties = mobile ? {} : { left: Math.max(80, Math.min(window.innerWidth - 80, x)), top: Math.max(8, y - 46) }
  return (
    <div className={`sel-bar ${mobile ? 'bottom' : ''}`} style={style} onMouseDown={e => e.preventDefault()} role="toolbar" aria-label="Highlight">
      <button type="button" className="sel-main" onClick={() => onPick('yellow')}><Icon name="highlighter" size={15} />Highlight</button>
      {COLORS.slice(1).map(c => <button key={c} type="button" className={`sel-dot hl-${c}`} onClick={() => onPick(c)} aria-label={`Highlight ${c}`} title={c} />)}
    </div>
  )
}

function Highlights({ a, st, busy, onNotes, onUpdate, onRemove }: { a: ArticleView; st: Settings | null; busy: boolean; onNotes: () => void; onUpdate: (h: Highlight, p: Partial<Highlight>) => void; onRemove: (h: Highlight) => void }) {
  const [edit, setEdit] = useState<{ id: number; note: string } | null>(null)
  const list = a.highlight_list || []
  if (list.length === 0) return null
  const coreOn = !!st && st.core_configured && st.core_registered !== false
  const notesOn = !!st && st.notes_mode !== 'off' && (st.notes_via !== '' || st.notes_mode === 'direct')
  return (
    <section className="reader-hls hl-skip" aria-label="Your highlights">
      <div className="row"><h3 className="grow">Your highlights <span className="muted">{list.length}</span></h3>
        <button type="button" className="btn sm" onClick={onNotes} disabled={busy || (!notesOn && !(st?.notes_mode === 'direct'))} title={notesOn ? 'Keep them as a note in Pocket Notes' : 'Connect Pocket Notes in Settings → Connected apps'}>
          {busy ? <Spinner /> : <Icon name="note" size={14} />}{a.notes_note_id ? 'Update the note' : 'Send to Pocket Notes'}
        </button>
      </div>
      {!notesOn && <p className="muted xsmall">{coreOn ? 'Switch on Pocket Notes in Settings → Connected apps to keep highlights as notes.' : <>Pocket Notes keeps highlights as notes. <CoreHint /></>}</p>}
      {list.map(h => (
        <div key={h.id} id={`hl-${h.id}`} className={`hl-card hl-c-${h.color}`}>
          <blockquote>{h.text}</blockquote>
          {edit?.id === h.id ? <form className="stack tight" onSubmit={e => { e.preventDefault(); onUpdate(h, { note: edit.note }); setEdit(null) }}>
            <textarea autoFocus rows={2} value={edit.note} onChange={e => setEdit({ id: h.id, note: e.target.value })} placeholder="Your thought on this…" />
            <div className="row"><button className="btn sm primary">Save</button><button type="button" className="btn sm ghost" onClick={() => setEdit(null)}>Cancel</button></div>
          </form> : h.note && <p className="hl-note">{h.note}</p>}
          <div className="hl-actions">
            <span className="row" style={{ gap: 4 }}>{COLORS.map(c => <button key={c} type="button" className={`sel-dot sm hl-${c} ${h.color === c ? 'on' : ''}`} aria-label={c} onClick={() => onUpdate(h, { color: c })} />)}</span>
            <span className="grow" />
            <button type="button" className="btn ghost xs" onClick={() => setEdit({ id: h.id, note: h.note })}>{h.note ? 'Edit note' : 'Add a note'}</button>
            <button type="button" className="btn ghost xs danger-text" onClick={() => onRemove(h)}>Remove</button>
          </div>
        </div>
      ))}
    </section>
  )
}

function NoteBox({ note, onSave }: { note: string; onSave: (n: string) => void }) {
  const [v, setV] = useState(note)
  const [open, setOpen] = useState(!!note)
  if (!open) return <div className="reader-note-open hl-skip"><button type="button" className="link-btn small" onClick={() => setOpen(true)}><Icon name="edit" size={13} />Add your own note on this article</button></div>
  return (
    <section className="reader-note hl-skip">
      <h3>Your note</h3>
      <textarea rows={3} value={v} onChange={e => setV(e.target.value)} onBlur={() => { if (v !== note) onSave(v) }} placeholder="What you think, what to do with it… Saved when you click away." />
    </section>
  )
}

function TextStyle({ st, onChange }: { st: Settings; onChange: (p: Partial<Settings>) => void }) {
  return (
    <div className="text-style">
      <div className="ts-row"><span>Size</span>
        <div className="row"><button type="button" className="btn sm icon-only" aria-label="Smaller" disabled={st.font_size <= 14} onClick={() => onChange({ font_size: st.font_size - 1 })}><Icon name="minus" size={14} /></button>
          <span className="tnum" style={{ width: 30, textAlign: 'center' }}>{st.font_size}</span>
          <button type="button" className="btn sm icon-only" aria-label="Larger" disabled={st.font_size >= 28} onClick={() => onChange({ font_size: st.font_size + 1 })}><Icon name="plus" size={14} /></button></div>
      </div>
      <div className="ts-row"><span>Font</span><Segmented size="sm" value={st.font} onChange={v => onChange({ font: v })} options={[{ value: 'serif', label: <span style={{ fontFamily: 'Georgia, serif' }}>Serif</span> }, { value: 'sans', label: 'Sans' }, { value: 'mono', label: <span style={{ fontFamily: 'var(--mono)' }}>Mono</span> }]} /></div>
      <div className="ts-row"><span>Width</span><Segmented size="sm" value={st.line_width} onChange={v => onChange({ line_width: v })} options={[{ value: 'narrow', label: 'Narrow' }, { value: 'medium', label: 'Medium' }, { value: 'wide', label: 'Wide' }]} /></div>
      <div className="ts-row"><span>Spacing</span><Segmented size="sm" value={st.line_height} onChange={v => onChange({ line_height: v })} options={[{ value: 'tight', label: 'Tight' }, { value: 'normal', label: 'Normal' }, { value: 'loose', label: 'Loose' }]} /></div>
      <div className="ts-row"><span>Page</span><div className="ts-themes">
        {(['app', 'paper', 'sepia', 'night'] as const).map(t => <button key={t} type="button" className={`ts-theme rt-${t} ${st.reader_theme === t ? 'on' : ''}`} onClick={() => onChange({ reader_theme: t })} title={{ app: 'Like the app', paper: 'Paper', sepia: 'Sepia', night: 'Night' }[t]}>Aa</button>)}
      </div></div>
      <div className="ts-row"><span>Justify</span><Switch size="sm" checked={st.justify} onChange={v => onChange({ justify: v })} label="Justify" /></div>
    </div>
  )
}
