// One box for everything: paste an address and Reader says what it can do
// with it — follow the site's feed (a blog, a newspaper, a YouTube channel,
// a feed address) and/or save the page to read later.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announce, hostOf } from '../lib/reader'
import type { Article, Discovery, Feed, Folder } from '../lib/types'
import { Icon } from './icons'
import { useFeeds } from './Layout'
import { Select } from './Select'
import { Modal, Spinner, Switch, useToast } from './ui'

const looksLikeURL = (s: string) => /^(https?:\/\/|feed:\/\/)?[\w-]+(\.[\w-]+)+(:\d+)?(\/\S*)?$/i.test(s.trim())

export function AddDialog({ initial, onClose, onDone }: { initial: string; onClose: () => void; onDone: () => void }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const { folders, reload } = useFeeds()
  const [url, setUrl] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [found, setFound] = useState<Discovery | null>(null)
  const [folder, setFolder] = useState('')
  const [fullText, setFullText] = useState(false)
  const [working, setWorking] = useState<string>('')
  const [extraFolders, setExtraFolders] = useState<Folder[]>([])
  const asked = useRef('')

  const look = async (raw = url) => {
    const v = raw.trim()
    if (!v || asked.current === v) return
    if (!looksLikeURL(v)) { setErr('Paste a web address, like nytimes.com or a link to an article.'); setFound(null); return }
    asked.current = v
    setBusy(true); setErr(''); setFound(null)
    try { setFound(await api.post<Discovery>(`${V1}/discover`, { url: v })) } catch (e) { setErr((e as Error).message); asked.current = '' } finally { setBusy(false) }
  }
  useEffect(() => { if (initial) look(initial) }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const follow = async (feedURL: string) => {
    setWorking(feedURL)
    try {
      const r = await api.post<{ feed: Feed; result?: { new: number } }>(`${V1}/feeds`, { url: feedURL, folder_id: folder ? Number(folder) : null, full_text: fullText })
      toast(`Following ${r.feed.custom_title || r.feed.title || hostOf(r.feed.url)}${r.result?.new ? ` — ${r.result.new} posts` : ''}`)
      announce(); onDone(); navigate(`/feed/${r.feed.id}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setWorking('') }
  }
  const save = async () => {
    if (!found) return
    setWorking('page')
    try {
      const r = await api.post<{ article: Article; created: boolean }>(`${V1}/saved`, { url: found.url })
      toast(r.created ? 'Saved to read later' : 'Already on your list — moved to the top')
      announce(); onDone(); navigate(`/a/${r.article.id}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setWorking('') }
  }
  const createFolder = async (name: string) => {
    const f = await api.post<Folder>(`${V1}/folders`, { name })
    setExtraFolders(x => [...x, f]); reload()
    return String(f.id)
  }
  const allFolders = [...folders, ...extraFolders.filter(x => !folders.some(f => f.id === x.id))]
  const kindLabel: Record<string, string> = { youtube: 'YouTube channel', rss: 'RSS feed', atom: 'Atom feed', json: 'JSON feed', web: 'Feed', podcast: 'Podcast' }

  return (
    <Modal title="Add" onClose={onClose} size="sm" className="add-modal">
      <form className="add-form" onSubmit={e => { e.preventDefault(); asked.current = ''; look() }}>
        <div className="add-input">
          <Icon name="link" size={16} className="muted" />
          <input autoFocus value={url} onChange={e => { setUrl(e.target.value); setErr('') }} onPaste={e => { const t = e.clipboardData.getData('text'); if (t) window.setTimeout(() => look(t), 0) }}
            placeholder="A website, a YouTube channel or an article" aria-label="Address" inputMode="url" autoCapitalize="off" autoCorrect="off" spellCheck={false} />
          <button className="btn primary sm" disabled={busy || !url.trim()}>{busy ? <Spinner /> : 'Look'}</button>
        </div>
      </form>
      {err && <div className="callout warn mt-s"><Icon name="info" size={15} /><span>{err}</span></div>}
      {!found && !err && !busy && <p className="muted small mt-s">Paste a site and Reader finds its feed, or a link to an article and Reader keeps a clean copy to read later.</p>}
      {busy && <p className="muted small mt-s row"><Spinner />Looking at {hostOf(url.trim().startsWith('http') ? url.trim() : 'https://' + url.trim())}…</p>}
      {found && (
        <div className="add-results">
          {found.feeds.length > 0 && <section>
            <h4 className="add-h">{found.feeds.length === 1 ? 'Follow' : 'Feeds on this site'}</h4>
            {found.feeds.map(f => (
              <div key={f.url} className="add-option">
                <span className="add-ico"><Icon name={f.kind === 'youtube' ? 'youtube' : 'rss'} size={18} /></span>
                <div className="grow">
                  <div className="add-title ellipsis">{f.title || hostOf(f.url)}</div>
                  <div className="muted xsmall ellipsis">{kindLabel[f.kind] || 'Feed'} · {f.url.replace(/^https?:\/\//, '')}</div>
                </div>
                {f.feed_id ? <button type="button" className="btn sm" onClick={() => { onDone(); navigate(`/feed/${f.feed_id}`) }}>Following</button>
                  : <button type="button" className="btn primary sm" disabled={!!working} onClick={() => follow(f.url)}>{working === f.url ? <Spinner /> : 'Follow'}</button>}
              </div>
            ))}
            {found.preview && found.preview.length > 0 && <ul className="add-preview">{found.preview.map(p => <li key={p.title} className="ellipsis">{p.title}</li>)}</ul>}
            {found.feeds.some(f => !f.feed_id) && <div className="add-opts">
              <label className="add-opt"><span className="muted small">Folder</span>
                <Select ariaLabel="Folder" value={folder} onChange={setFolder} placeholder="No folder" clearLabel="No folder" options={allFolders.map(f => ({ value: String(f.id), label: f.name }))} onCreate={createFolder} createLabel="New folder" />
              </label>
              {found.feeds.every(f => f.kind !== 'youtube') && <div className="add-opt row"><Switch size="sm" checked={fullText} onChange={setFullText} label="Fetch whole articles" /><span className="small">Fetch whole articles <span className="muted">— for feeds that send only the first lines</span></span></div>}
            </div>}
          </section>}
          {found.page && <section>
            <h4 className="add-h">Read later</h4>
            <div className="add-option page">
              {found.page.image_url ? <img src={found.page.image_url} alt="" className="add-thumb" referrerPolicy="no-referrer" loading="lazy" onError={e => ((e.target as HTMLElement).style.display = 'none')} /> : <span className="add-ico"><Icon name="text" size={18} /></span>}
              <div className="grow">
                <div className="add-title">{found.page.title || hostOf(found.url)}</div>
                <div className="muted xsmall ellipsis">{found.page.site_name || hostOf(found.url)}</div>
                {found.page.excerpt && <div className="muted small add-excerpt">{found.page.excerpt}</div>}
              </div>
            </div>
            <div className="row mt-s">
              {found.page.saved_id ? <button type="button" className="btn" onClick={() => { onDone(); navigate(`/a/${found.page!.saved_id}`) }}><Icon name="bookmark-check" size={15} />Already saved — open</button>
                : <button type="button" className={`btn ${found.feeds.length ? '' : 'primary'}`} disabled={!!working} onClick={save}>{working === 'page' ? <Spinner /> : <Icon name="bookmark" size={15} />}Save to read later</button>}
            </div>
          </section>}
          {found.feeds.length === 0 && !found.page && <p className="muted">Nothing to follow or save at that address.</p>}
          {found.page && found.feeds.length === 0 && <p className="muted xsmall mt-s">This page offers no feed. To follow a site that has none, an RSS-Bridge can make one.</p>}
        </div>
      )}
      <AddFooter onClose={onClose} />
    </Modal>
  )
}

function AddFooter({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  return (
    <div className="add-foot">
      <button type="button" className="link-btn small" onClick={() => { onClose(); navigate('/settings/data') }}><Icon name="upload" size={13} />Import from FreshRSS, Feedly, Karakeep, bookmarks…</button>
      <button type="button" className="link-btn small" onClick={() => { onClose(); navigate('/settings/tokens') }}><Icon name="smartphone" size={13} />Save from your phone</button>
    </div>
  )
}

