// Manage what you follow: feeds grouped by folder with their switches, and
// the rules that act on new posts.
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announce, feedName, groupFeeds, hostOf, openAdd, since } from '../lib/reader'
import type { Feed, Folder, Rule } from '../lib/types'
import { Icon } from '../components/icons'
import { useFeeds } from '../components/Layout'
import { Select } from '../components/Select'
import { FeedIcon } from '../components/Sidebar'
import { Confirm, Empty, Field, Loading, type MenuItem, MenuButton, Modal, PageHeader, Segmented, Spinner, Switch, Tabs, useAsync, useToast } from '../components/ui'

export default function Feeds() {
  const { tab } = useParams()
  const navigate = useNavigate()
  const t = tab === 'rules' ? 'rules' : 'feeds'
  return (
    <div className="feeds-page">
      <PageHeader title="Feeds" sub="What you follow, and what happens to new posts." />
      <Tabs tabs={[{ id: 'feeds', label: 'Feeds', icon: 'rss' }, { id: 'rules', label: 'Rules', icon: 'sliders' }]} value={t} onChange={v => navigate(v === 'rules' ? '/feeds/rules' : '/feeds', { replace: true })} />
      <div className="mt">{t === 'rules' ? <Rules /> : <FeedList />}</div>
    </div>
  )
}

function FeedList() {
  const { feeds, folders, loaded, reload } = useFeeds()
  const { toast } = useToast()
  const [sp, setSp] = useSearchParams()
  const [edit, setEdit] = useState<Feed | null>(null)
  const [folderEdit, setFolderEdit] = useState<Folder | 'new' | null>(null)
  const [delFolder, setDelFolder] = useState<Folder | null>(null)
  const [importing, setImporting] = useState(false)
  useEffect(() => { const id = Number(sp.get('edit')); if (id && feeds.length) { const f = feeds.find(x => x.id === id); if (f) setEdit(f); setSp(new URLSearchParams(), { replace: true }) } }, [sp, feeds]) // eslint-disable-line react-hooks/exhaustive-deps
  const toggle = async (f: Feed, p: Partial<Feed>) => { try { await api.put(`${V1}/feeds/${f.id}`, p); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const importFile = async (file: File) => {
    setImporting(true)
    try {
      const r = await api.post<{ kind: string; counts: Record<string, number> }>(`${V1}/import`, await file.text())
      const c = r.counts
      toast(r.kind === 'opml' ? `${c.feeds || 0} feeds added${c.already ? `, ${c.already} already followed` : ''} — checking them now` : `Imported: ${Object.entries(c).filter(([, v]) => v).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new'}`)
      announce(); window.setTimeout(reload, 5000)
    } catch (e) { toast((e as Error).message, 'err') } finally { setImporting(false) }
  }
  const removeFolder = async (f: Folder) => { setDelFolder(null); try { await api.del(`${V1}/folders/${f.id}`); reload(); toast('Folder removed; its feeds stay') } catch (e) { toast((e as Error).message, 'err') } }
  if (!loaded) return <Loading />
  const { groups, loose } = groupFeeds(feeds, folders)
  const failing = feeds.filter(f => f.error && f.error_count >= 3)
  const row = (f: Feed) => (
    <div key={f.id} className={`feed-row ${f.paused ? 'off' : ''}`}>
      <FeedIcon feed={f} size={18} />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="row" style={{ gap: 6 }}><Link to={`/feed/${f.id}`} className="feed-name ellipsis">{feedName(f)}</Link>{f.paused && <span className="badge">paused</span>}{f.full_text && <span className="badge" title="Whole articles are fetched">full text</span>}</div>
        <div className="muted xsmall ellipsis">{f.error && f.error_count >= 3 ? <span className="bad">{f.error}</span> : <>{hostOf(f.site_url || f.url)} · {f.total} posts{f.unread ? `, ${f.unread} unread` : ''}{f.newest_at ? ` · newest ${since(f.newest_at)}` : ''}</>}</div>
      </div>
      <span className="feed-switch" title="Push a notification for each new post"><Icon name="bell" size={13} className="muted" /><Switch size="sm" checked={f.notify} onChange={v => toggle(f, { notify: v })} label="Notify" /></span>
      <button type="button" className="btn ghost sm" onClick={() => setEdit(f)}>Edit</button>
    </div>
  )
  return (
    <div className="stack loose">
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => openAdd()}><Icon name="plus" size={15} />Follow a website</button>
        <button type="button" className="btn" onClick={() => setFolderEdit('new')}><Icon name="folder" size={15} />New folder</button>
        <label className="btn">{importing ? <Spinner /> : <Icon name="upload" size={15} />}Import OPML…<input type="file" accept=".opml,.xml,text/xml,application/xml" hidden onChange={e => e.target.files?.[0] && importFile(e.target.files[0])} /></label>
        <a className="btn ghost" href={`${V1}/export.opml`}><Icon name="download" size={15} />Export OPML</a>
      </div>
      {failing.length > 0 && <div className="callout warn"><Icon name="alert" size={15} /><div>{failing.length === 1 ? 'One feed is' : `${failing.length} feeds are`} not working: {failing.map(feedName).join(', ')}. Reader keeps trying, less often each time; edit the address if the site moved.</div></div>}
      {feeds.length === 0 ? <Empty title="You follow nothing yet" icon="rss">Paste a website and Reader finds its feed. Moving from FreshRSS, Feedly or another reader? Import its OPML file.</Empty> : <>
        {groups.map(g => (
          <section key={g.folder.id} className="feed-group">
            <div className="feed-group-head">
              <Icon name="folder" size={15} className="muted" /><h3 className="grow">{g.folder.name}</h3><span className="muted small">{g.feeds.length}</span>
              <MenuButton label="Folder" align="right" items={[{ label: 'Rename', icon: 'edit', onSelect: () => setFolderEdit(g.folder) }, { label: 'Remove folder', icon: 'trash', danger: true, onSelect: () => setDelFolder(g.folder) }] as MenuItem[]}><Icon name="more" size={16} /></MenuButton>
            </div>
            {g.feeds.length ? g.feeds.map(row) : <p className="muted small feed-empty">No feeds in this folder.</p>}
          </section>
        ))}
        {loose.length > 0 && <section className="feed-group"><div className="feed-group-head"><Icon name="rss" size={15} className="muted" /><h3 className="grow">Not in a folder</h3><span className="muted small">{loose.length}</span></div>{loose.map(row)}</section>}
      </>}
      {edit && <FeedModal feed={edit} folders={folders} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload(); announce() }} />}
      {folderEdit && <FolderModal folder={folderEdit === 'new' ? undefined : folderEdit} onClose={() => setFolderEdit(null)} onSaved={() => { setFolderEdit(null); reload() }} />}
      {delFolder && <Confirm title={`Remove “${delFolder.name}”?`} message="The folder goes; its feeds stay, outside any folder." confirmLabel="Remove" onCancel={() => setDelFolder(null)} onConfirm={() => removeFolder(delFolder)} />}
    </div>
  )
}

const EVERY = [{ value: 0, label: 'Default' }, { value: 15, label: '15 min' }, { value: 60, label: 'Hourly' }, { value: 360, label: '6 h' }, { value: 1440, label: 'Daily' }]

function FeedModal({ feed, folders, onClose, onSaved }: { feed: Feed; folders: Folder[]; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const navigate = useNavigate()
  const [f, setF] = useState({ custom_title: feed.custom_title, url: feed.url, folder_id: feed.folder_id ? String(feed.folder_id) : '', full_text: feed.full_text, notify: feed.notify, paused: feed.paused, refresh_min: feed.refresh_min })
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const [extra, setExtra] = useState<Folder[]>([])
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.put(`${V1}/feeds/${feed.id}`, { ...f, folder_id: f.folder_id ? Number(f.folder_id) : null }); toast('Saved'); onSaved() } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => {
    setDel(false)
    try { await api.del(`${V1}/feeds/${feed.id}`); toast(`Unfollowed ${feedName(feed)}`); onSaved(); navigate('/feeds') } catch (err) { toast((err as Error).message, 'err') }
  }
  const createFolder = async (name: string) => { const x = await api.post<Folder>(`${V1}/folders`, { name }); setExtra(e => [...e, x]); return String(x.id) }
  const all = [...folders, ...extra.filter(x => !folders.some(y => y.id === x.id))]
  return (
    <Modal title={feedName(feed)} onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name" help={feed.title && f.custom_title ? `The feed calls itself “${feed.title}”.` : 'Leave empty to use the feed’s own name.'}><input value={f.custom_title} onChange={e => setF({ ...f, custom_title: e.target.value })} placeholder={feed.title} /></Field>
        <Field group label="Folder"><Select ariaLabel="Folder" value={f.folder_id} onChange={v => setF({ ...f, folder_id: v })} placeholder="No folder" clearLabel="No folder" options={all.map(x => ({ value: String(x.id), label: x.name }))} onCreate={createFolder} createLabel="New folder" /></Field>
        <Field label="Feed address" help={feed.site_url ? <>Website: <a href={feed.site_url} target="_blank" rel="noreferrer">{hostOf(feed.site_url)}</a></> : undefined}><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} inputMode="url" spellCheck={false} /></Field>
        <Field group label="Check every"><Segmented size="sm" value={f.refresh_min} onChange={v => setF({ ...f, refresh_min: v })} options={EVERY} /></Field>
        <div className="stack tight">
          <div className="row"><Switch checked={f.full_text} onChange={v => setF({ ...f, full_text: v })} label="Fetch whole articles" /><span>Fetch whole articles <span className="muted small">— for feeds that send only the first lines</span></span></div>
          <div className="row"><Switch checked={f.notify} onChange={v => setF({ ...f, notify: v })} label="Notify me" /><span>Notify me about new posts</span></div>
          <div className="row"><Switch checked={f.paused} onChange={v => setF({ ...f, paused: v })} label="Paused" /><span>Pause <span className="muted small">— stop checking for now</span></span></div>
        </div>
        {feed.error && <div className="callout warn"><Icon name="alert" size={15} /><span>Last check: {feed.error}</span></div>}
        <p className="muted xsmall">Followed since {new Date(feed.created_at).toLocaleDateString()} · last checked {feed.fetched_at ? since(feed.fetched_at) + ' ago' : 'not yet'}{feed.source === 'import' ? ' · imported' : ''}</p>
        <div className="form-actions"><button type="button" className="btn ghost danger-text" onClick={() => setDel(true)} style={{ marginRight: 'auto' }}>Unfollow</button><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
      {del && <Confirm title={`Unfollow ${feedName(feed)}?`} message="Its posts go too, except the ones you starred, saved, highlighted or noted: those stay as saved pages." confirmLabel="Unfollow" onCancel={() => setDel(false)} onConfirm={remove} />}
    </Modal>
  )
}

function FolderModal({ folder, onClose, onSaved }: { folder?: Folder; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [name, setName] = useState(folder?.name || '')
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    try { if (folder) await api.put(`${V1}/folders/${folder.id}`, { name }); else await api.post(`${V1}/folders`, { name }); onSaved() } catch (err) { toast((err as Error).message, 'err') }
  }
  return (
    <Modal title={folder ? 'Rename folder' : 'New folder'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input autoFocus value={name} onChange={e => setName(e.target.value)} maxLength={80} required placeholder="News, Friends, YouTube…" /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">Save</button></div>
      </form>
    </Modal>
  )
}

// ---- rules ----

const ACTION: Record<Rule['action'], [string, string]> = { read: ['Mark as read', 'check'], star: ['Star', 'star'], save: ['Save to read later', 'bookmark'], tag: ['Add a tag', 'tag'], notify: ['Notify me', 'bell'] }
const FIELD: Record<Rule['field'], string> = { title: 'the title', any: 'the title or text', author: 'the author', url: 'the address' }

function Rules() {
  const { toast } = useToast()
  const { feeds, folders } = useFeeds()
  const { data, reload } = useAsync(() => api.get<Rule[]>(`${V1}/rules`), [])
  const [edit, setEdit] = useState<Rule | 'new' | null>(null)
  const toggle = async (r: Rule) => { try { await api.put(`${V1}/rules/${r.id}`, { ...r, enabled: !r.enabled }); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async (r: Rule) => { try { await api.del(`${V1}/rules/${r.id}`); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const where = (r: Rule) => r.feed_id ? feedName(feeds.find(f => f.id === r.feed_id)) : r.folder_id ? folders.find(f => f.id === r.folder_id)?.name : 'any feed'
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>Rules look at each new post as it arrives. Hide sponsored posts, star anything that mentions your town, tag posts about a topic, or get a push when a release note appears.</div></div>
      <div className="row"><button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Add a rule</button></div>
      {!data ? <Loading /> : data.length === 0 ? <Empty title="No rules yet" icon="sliders">New posts arrive just as the feeds send them.</Empty> : (
        <div className="rule-list">
          {data.map(r => (
            <div key={r.id} className={`rule-row ${r.enabled ? '' : 'off'}`}>
              <Switch size="sm" checked={r.enabled} onChange={() => toggle(r)} label="Enabled" />
              <span className="rule-ico"><Icon name={ACTION[r.action][1]} size={15} /></span>
              <div className="grow" style={{ minWidth: 0 }}>
                <div className="ellipsis"><b>{r.name || ACTION[r.action][0]}</b></div>
                <div className="muted small ellipsis">When {FIELD[r.field]} mentions {r.words.split(',').map(w => `“${w.trim()}”`).join(' or ')} in {where(r)} → {ACTION[r.action][0].toLowerCase()}{r.action === 'tag' ? ` #${r.tag}` : ''}</div>
              </div>
              <span className="muted xsmall tnum" title="Posts it acted on">{r.hits}×</span>
              <button type="button" className="btn ghost sm" onClick={() => setEdit(r)}>Edit</button>
              <button type="button" className="btn ghost sm danger-text" onClick={() => remove(r)}>Remove</button>
            </div>
          ))}
        </div>
      )}
      {edit && <RuleModal rule={edit === 'new' ? undefined : edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
    </div>
  )
}

function RuleModal({ rule, onClose, onSaved }: { rule?: Rule; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const { feeds, folders } = useFeeds()
  const [f, setF] = useState({ name: rule?.name || '', field: rule?.field || 'title', words: rule?.words || '', action: rule?.action || 'read', tag: rule?.tag || '', where: rule?.feed_id ? `feed:${rule.feed_id}` : rule?.folder_id ? `folder:${rule.folder_id}` : '' })
  const [test, setTest] = useState<{ matches: number; of: number; examples: { id: number; title: string; feed: string }[] } | null>(null)
  const [busy, setBusy] = useState(false)
  const body = () => ({ name: f.name, field: f.field, words: f.words, action: f.action, tag: f.tag, enabled: rule?.enabled ?? true,
    feed_id: f.where.startsWith('feed:') ? Number(f.where.slice(5)) : null, folder_id: f.where.startsWith('folder:') ? Number(f.where.slice(7)) : null })
  const tryIt = async () => { try { setTest(await api.post(`${V1}/rules/test`, body())) } catch (e) { toast((e as Error).message, 'err') } }
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (rule) await api.put(`${V1}/rules/${rule.id}`, body()); else await api.post(`${V1}/rules`, body()); onSaved() } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const whereOpts = [{ value: '', label: 'Any feed' }, ...folders.map(x => ({ value: `folder:${x.id}`, label: x.name, group: 'Folders' })), ...feeds.map(x => ({ value: `feed:${x.id}`, label: feedName(x), group: 'Feeds' }))]
  return (
    <Modal title={rule ? 'Edit rule' : 'New rule'} onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="When the post mentions" help="Words or phrases, separated by commas; any of them matches. Not case-sensitive."><input autoFocus value={f.words} onChange={e => { setF({ ...f, words: e.target.value }); setTest(null) }} placeholder="sponsored, advertisement" required /></Field>
        <div className="two-col">
          <Field group label="Look in"><Select ariaLabel="Look in" value={f.field} onChange={v => { setF({ ...f, field: v as Rule['field'] }); setTest(null) }} options={Object.entries(FIELD).map(([value, label]) => ({ value, label: label[0].toUpperCase() + label.slice(1) }))} /></Field>
          <Field group label="In"><Select ariaLabel="Where" value={f.where} onChange={v => { setF({ ...f, where: v }); setTest(null) }} options={whereOpts} /></Field>
        </div>
        <Field group label="Then"><Segmented size="sm" value={f.action} onChange={v => setF({ ...f, action: v as Rule['action'] })} options={(Object.keys(ACTION) as Rule['action'][]).map(k => ({ value: k, label: ACTION[k][0] }))} /></Field>
        {f.action === 'tag' && <Field label="Tag"><input value={f.tag} onChange={e => setF({ ...f, tag: e.target.value })} placeholder="transport" required /></Field>}
        {f.action === 'notify' && <p className="muted small">Pushes go to the address in Settings → Notifications.</p>}
        <Field label="Name (optional)"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="Hide sponsored posts" /></Field>
        <div className="rule-test">
          <button type="button" className="btn sm" onClick={tryIt} disabled={!f.words.trim()}><Icon name="search" size={14} />Try on recent posts</button>
          {test && <span className="small">{test.matches ? `${test.matches} of the last ${test.of} posts match` : `None of the last ${test.of} posts match`}</span>}
          {test && test.examples.length > 0 && <ul className="rule-examples">{test.examples.map(x => <li key={x.id} className="ellipsis small"><span className="muted">{x.feed}:</span> {x.title}</li>)}</ul>}
        </div>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Modal>
  )
}
