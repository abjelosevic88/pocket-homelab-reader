import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { bookmarklet } from '../lib/reader'
import { Avatar, Confirm, CopyField, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'

type TabId = 'reading' | 'feeds' | 'notifications' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'reading', label: 'Reading', icon: 'type', sub: 'How articles look, how lists behave, and what opening or scrolling does.' },
  { id: 'feeds', label: 'Feeds & clean-up', icon: 'rss', sub: 'How often feeds are checked, whole articles for saved pages, and how long old posts stay.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'New posts from the feeds you choose, a morning digest, and a word when a feed stops working.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Notes keeps your highlights as notes; other Pocket apps can read what you read and save pages here.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who writes summaries, the catch-up briefing and tags: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Phone RSS apps, the bookmarklet, the share sheet, and tokens for scripts and widgets.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Import from FreshRSS, Feedly, Karakeep, Pocket or your bookmarks; export everything.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'reading'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'reading' && <Reading />}
            {tab === 'feeds' && <FeedSettings />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const DAYS: [number, string][] = [[1, 'Monday'], [2, 'Tuesday'], [3, 'Wednesday'], [4, 'Thursday'], [5, 'Friday'], [6, 'Saturday'], [7, 'Sunday']]
const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }))

function Reading() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Text" icon="type" sub="The same settings are in the Aa button of every article.">
        <div className={`ts-sample rt-${f.reader_theme} f-${f.font} lh-${f.line_height}`} style={{ fontSize: f.font_size }}><b>The quiet joy of reading.</b> One place for the sites you follow and the pages you keep, without the page around them.</div>
        <SettingRow title="Size"><div className="row"><input type="range" min={14} max={28} value={f.font_size} onChange={e => setF({ ...f, font_size: Number(e.target.value) })} aria-label="Text size" /><span className="tnum small">{f.font_size}px</span></div></SettingRow>
        <SettingRow title="Font"><Segmented size="sm" value={f.font} onChange={v => setF({ ...f, font: v })} options={[{ value: 'serif', label: 'Serif' }, { value: 'sans', label: 'Sans' }, { value: 'mono', label: 'Mono' }]} /></SettingRow>
        <SettingRow title="Line width"><Segmented size="sm" value={f.line_width} onChange={v => setF({ ...f, line_width: v })} options={[{ value: 'narrow', label: 'Narrow' }, { value: 'medium', label: 'Medium' }, { value: 'wide', label: 'Wide' }]} /></SettingRow>
        <SettingRow title="Line spacing"><Segmented size="sm" value={f.line_height} onChange={v => setF({ ...f, line_height: v })} options={[{ value: 'tight', label: 'Tight' }, { value: 'normal', label: 'Normal' }, { value: 'loose', label: 'Loose' }]} /></SettingRow>
        <SettingRow title="Page colour" help="“Like the app” follows light and dark."><Segmented size="sm" value={f.reader_theme} onChange={v => setF({ ...f, reader_theme: v })} options={[{ value: 'app', label: 'Like the app' }, { value: 'paper', label: 'Paper' }, { value: 'sepia', label: 'Sepia' }, { value: 'night', label: 'Night' }]} /></SettingRow>
        <SettingRow title="Justify text"><Switch checked={f.justify} onChange={v => setF({ ...f, justify: v })} label="Justify" /></SettingRow>
      </Section>
      <Section title="Lists" icon="list">
        <SettingRow title="Look"><Segmented size="sm" value={f.list_style} onChange={v => setF({ ...f, list_style: v })} options={[{ value: 'cards', label: 'Cards' }, { value: 'magazine', label: 'Magazine' }, { value: 'compact', label: 'Titles only' }]} /></SettingRow>
        <SettingRow title="Pictures in lists"><Switch checked={f.show_images} onChange={v => setF({ ...f, show_images: v })} label="Pictures" /></SettingRow>
        <SettingRow title="Oldest first" help="Unread and Read later start with what has waited longest."><Switch checked={f.sort_oldest} onChange={v => setF({ ...f, sort_oldest: v })} label="Oldest first" /></SettingRow>
        <SettingRow title="Feeds show only unread" help="Off: a feed shows its read posts too, greyed."><Switch checked={f.hide_read} onChange={v => setF({ ...f, hide_read: v })} label="Only unread" /></SettingRow>
      </Section>
      <Section title="Marking as read" icon="check-circle">
        <SettingRow title="When I open a post"><Switch checked={f.mark_on_open} onChange={v => setF({ ...f, mark_on_open: v })} label="Mark on open" /></SettingRow>
        <SettingRow title="When I scroll past it" help="Posts that leave the top of the list are marked read — for skimming."><Switch checked={f.mark_on_scroll} onChange={v => setF({ ...f, mark_on_scroll: v })} label="Mark on scroll" /></SettingRow>
        <SettingRow title="Ask before “Mark all as read”"><Switch checked={f.confirm_all} onChange={v => setF({ ...f, confirm_all: v })} label="Confirm" /></SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

const EVERY = [{ value: '15', label: 'Every 15 minutes' }, { value: '30', label: 'Every 30 minutes' }, { value: '60', label: 'Every hour' }, { value: '180', label: 'Every 3 hours' }, { value: '720', label: 'Twice a day' }, { value: '1440', label: 'Once a day' }]
const KEEP = [{ value: '7', label: 'a week' }, { value: '30', label: 'a month' }, { value: '90', label: '3 months' }, { value: '365', label: 'a year' }, { value: '0', label: 'forever' }]

function FeedSettings() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  const every = EVERY.some(e => e.value === String(f.refresh_min)) ? EVERY : [...EVERY, { value: String(f.refresh_min), label: `Every ${f.refresh_min} minutes` }]
  const keep = (v: number) => KEEP.some(e => e.value === String(v)) ? KEEP : [...KEEP, { value: String(v), label: `${v} days` }]
  return (
    <div className="stack loose">
      <Section title="Checking feeds" icon="refresh">
        <SettingRow title="Check feeds" help="Each feed can have its own pace (Feeds → Edit). Feeds that fail are tried less and less often, at most once a day."><Select ariaLabel="Check every" value={String(f.refresh_min)} onChange={v => setF({ ...f, refresh_min: Number(v) })} options={every} /></SettingRow>
        <SettingRow title="Keep whole articles of saved pages" help="When you save a page, Reader keeps a clean copy of its text — to read offline-quiet, search and highlight. Off: only the link."><Switch checked={f.auto_extract} onChange={v => setF({ ...f, auto_extract: v })} label="Whole articles" /></SettingRow>
        {!f.fetch_private && <p className="muted xsmall">Feeds on your own network (an RSS-Bridge, a NAS) are blocked on this server for safety; the admin can allow them with <code>FETCH_PRIVATE=true</code>.</p>}
      </Section>
      <Section title="Clean-up" icon="archive" sub="Old feed posts are removed so lists stay quick. Starred posts, your read-later list, and anything highlighted or with a note are never removed.">
        <SettingRow title="Remove read posts after"><Select ariaLabel="Read posts" value={String(f.keep_read_days)} onChange={v => setF({ ...f, keep_read_days: Number(v) })} options={keep(f.keep_read_days)} /></SettingRow>
        <SettingRow title="Remove unread posts after" help="Posts you never got to."><Select ariaLabel="Unread posts" value={String(f.keep_unread_days)} onChange={v => setF({ ...f, keep_unread_days: Number(v) })} options={keep(f.keep_unread_days)} /></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const digest = async () => { try { const r = await api.post<{ ok: boolean; sent?: boolean; error?: string; report?: string }>(`${V1}/notify/digest`); if (r.ok) toast(r.sent ? 'Digest sent' : (r.report || 'Nothing to send').split('\n')[0]); else toast(r.error || 'failed', 'err') } catch (e) { toast((e as Error).message, 'err') } }
  const toggleDay = (d: number) => { const s = String(d); setF({ ...f, digest_days: f.digest_days.includes(s) ? f.digest_days.replace(s, '') : [...f.digest_days, s].sort().join('') }) }
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where notifications go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/reader" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="What to send" icon="newspaper">
        <SettingRow title="New posts" help="From the feeds you switch on in Feeds (the bell), and from rules that say “notify me”. Several posts at once come as one message."><Switch checked={f.notify_new_posts} onChange={v => setF({ ...f, notify_new_posts: v })} label="New posts" /></SettingRow>
        <SettingRow title="Morning digest" help="What arrived since yesterday, by feed, and how much is on your read-later list.">
          <div className="row wrap"><Switch checked={f.digest_enabled} onChange={v => setF({ ...f, digest_enabled: v })} label="Digest" />{f.digest_enabled && <Select ariaLabel="Digest hour" value={String(f.digest_hour)} onChange={v => setF({ ...f, digest_hour: Number(v) })} options={HOURS} />}</div>
        </SettingRow>
        {f.digest_enabled && <SettingRow title="On" stack><div className="row wrap">{DAYS.map(([d, l]) => <button key={d} type="button" className={`chip day-chip ${f.digest_days.includes(String(d)) ? 'on' : ''}`} onClick={() => toggleDay(d)}>{l.slice(0, 3)}</button>)}</div></SettingRow>}
        <SettingRow title="A feed stopped working" help="Once, after a day of failed tries."><Switch checked={f.notify_feed_errors} onChange={v => setF({ ...f, notify_feed_errors: v })} label="Feed errors" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        <button type="button" className="btn" onClick={digest}><Icon name="newspaper" size={15} />Send the digest now</button>
        {status('notify')}
      </div>
    </div>
  )
}

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Reader acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      <Section title="Pocket Notes — keep your highlights" icon="note" actions={connBadge(eff(f.notes_mode), f.notes_via)}
        sub="Each article’s highlights become one note, with your thoughts under each passage and a link back. Sending again updates the same note.">
        <Connection app="Pocket Notes" mode={f.notes_mode} via={f.notes_via} detail={f.notes_detail} core={coreOn} hasToken={!!f.notes_token}
          onMode={m => save({ notes_mode: m })} onDisconnect={() => save({ notes_url: '', notes_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('notes', f.notes_url, f.notes_token)}>Test connection</button>{status('notes')}</>}
          direct={<div className="two-col tight">
            <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f.notes_url} onChange={e => setF({ ...f, notes_url: e.target.value })} placeholder="http://host.docker.internal:8182" /></Field>
            <Field label="API token" help="Pocket Notes → Settings → API tokens"><input value={f.notes_token} onChange={e => setF({ ...f, notes_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
        {eff(f.notes_mode) !== 'off' && <div className="mt">
          <SettingRow title="Folder in Pocket Notes" help="Where the notes go; made when missing."><input value={f.notes_folder} onChange={e => setF({ ...f, notes_folder: e.target.value })} placeholder="Reader" className="set-short" /></SettingRow>
          <SettingRow title="Send as I highlight" help="Off: press “Send to Pocket Notes” under an article’s highlights."><Switch checked={f.notes_auto} onChange={v => setF({ ...f, notes_auto: v })} label="Automatic" /></SettingRow>
        </div>}
      </Section>
      <Section title="Other Pocket apps" icon="layers" sub="Reader offers your reading to the rest of the box through Pocket Core. Each app asks for it and you switch it on in Pocket Core → Links. Your own notes on articles never leave this app.">
        <dl className="set-dl">
          <div><dt>reading:read</dt><dd className="muted">What you read, saved and highlighted (Pocket Day writes it into your journal), your read-later list and feeds.</dd></div>
          <div><dt>reading:write</dt><dd className="muted">Save pages to read later from another app (a link in Pocket Clips, a page in a project); an app may remove only what it saved.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Summarises an article (the ✦ button), writes a short “catch me up” briefing of what is unread, suggests tags for pages you save, and answers questions about what you read. Only the text of those articles is sent, and only when you ask — except tags, if you switch them on below.">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <Section title="Options" icon="sliders">
        <SettingRow title="Tag pages I save" help="Up to three tags, preferring the ones you already use (like Karakeep’s AI tags)."><Switch checked={f.ai_auto_tag} onChange={v => setF({ ...f, ai_auto_tag: v })} label="Auto tags" /></SettingRow>
        <SettingRow title="Language of summaries" help="Empty: the article’s own language."><input value={f.ai_language} onChange={e => setF({ ...f, ai_language: e.target.value })} placeholder="English" className="set-short" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Reader does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own feeds, saved pages, highlights and connections. Admins manage accounts; they do not see anyone else’s reading.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their feeds, saved pages and highlights." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings, user } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent | null, n?: string) => {
    e?.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: n || name || 'token' }); setFresh(r.token); setName(''); reload() } catch (err) { toast((err as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Phone and desktop RSS apps" icon="smartphone" sub="Reeder, NetNewsWire, ReadKit, FeedMe, Fluent Reader, Read You and others sync with Reader the way they do with FreshRSS (the Google Reader API). Feeds, folders, read and starred stay the same everywhere.">
        <dl className="set-dl">
          <div><dt>Account type</dt><dd>FreshRSS, or “Google Reader API”</dd></div>
          <div><dt>Server</dt><dd><CopyField value={`${base}/api/greader.php`} /></dd></div>
          <div><dt>User name</dt><dd><code>{user?.email}</code></dd></div>
          <div><dt>Password</dt><dd>a personal token from below — <button type="button" className="link-btn" onClick={() => create(null, 'Phone app')}>make one for your phone</button></dd></div>
        </dl>
        <p className="muted xsmall mt-s">Pages on your read-later list show up in those apps under the label “Read later”.</p>
      </Section>
      <Section title="Save from anywhere" icon="bookmark" sub="Three ways to put a page on your read-later list without opening Reader.">
        <div className="stack">
          <div><b className="small">Bookmarklet</b> <span className="muted small">— drag this to your bookmarks bar, then press it on any page:</span>
            <div className="row mt-s"><a className="btn sm bookmarklet" href={bookmarklet(base)} onClick={e => { e.preventDefault(); toast('Drag the button to your bookmarks bar') }}><Icon name="bookmark" size={14} />Save to Reader</a></div></div>
          <div><b className="small">iPhone and iPad</b> <span className="muted small">— add Reader to the home screen (Share → Add to Home Screen); then a Shortcut with “Open URLs” on</span>
            <CopyField value={`${base}/save?url=[Shortcut Input]&source=share`} /><span className="muted xsmall">appears in every share sheet as “Save to Reader”.</span></div>
          <div><b className="small">Any app or script</b> <span className="muted small">— send a link with a token:</span>
            <pre className="mono small set-pre">{`curl -X POST ${base}/api/v1/saved \\\n  -H "Authorization: Bearer prd_…" \\\n  -d '{"url":"https://example.com/article"}'`}</pre></div>
        </div>
      </Section>
      <Section title="Personal tokens" icon="key" sub={<>A token acts as you: it reads and changes everything. Use it as a phone app’s password, in <code>Authorization: Bearer &lt;token&gt;</code>, or for a Homepage widget (<code>GET /api/v1/dashboard</code>).</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Reeder on my phone)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately, phone apps included." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>; see <code>docs/api.md</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>POST /saved</code></dt><dd className="muted"><code>{'{"url":"…","tags":"later"}'}</code> — save a page to read later.</dd></div>
          <div><dt><code>POST /feeds</code></dt><dd className="muted"><code>{'{"url":"example.com"}'}</code> — follow a site (its feed is found).</dd></div>
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Unread, read later, starred, highlights — for Homepage.</dd></div>
          <div><dt><code>GET /articles?view=unread</code></dt><dd className="muted">Any list: unread, saved, starred, history, <code>q=</code> search, <code>feed=</code>, <code>tag=</code>.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">One day’s reading — what Pocket Day reads.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const [last, setLast] = useState('')
  const importFile = async (file: File) => {
    setBusy(true); setLast('')
    try {
      const r = await upload<{ kind: string; counts: Record<string, number> }>(`${V1}/import`, 'file', file)
      const kinds: Record<string, string> = { opml: 'Subscriptions (OPML)', 'pocket-reader': 'Pocket Reader export', karakeep: 'Karakeep export', bookmarks: 'Browser bookmarks', csv: 'A list of links (CSV)' }
      const parts = Object.entries(r.counts).filter(([, v]) => v).map(([k, v]) => `${v} ${k === 'already' ? 'already there' : k}`)
      setLast(`${kinds[r.kind] || r.kind}: ${parts.join(', ') || 'nothing new'}.`)
      toast('Imported'); window.dispatchEvent(new CustomEvent('rd:changed'))
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Import" icon="upload" sub="Moving from another app? Reader recognises the file by itself. Importing the same file twice adds nothing. Saved links get their text fetched in the background.">
        <ul className="set-list small">
          <li><b>FreshRSS, Feedly, Inoreader, NetNewsWire, any reader</b> — the subscriptions file (OPML).</li>
          <li><b>Karakeep / Hoarder</b> — Settings → Import / Export → Export (JSON): links, tags, notes, favourites, archive.</li>
          <li><b>Browser bookmarks, Raindrop, Instapaper, Pocket</b> — the bookmarks HTML file; folders become tags.</li>
          <li><b>Omnivore, Readwise, Pocket CSV</b> — a CSV with a <code>url</code> column (and <code>title</code>, <code>tags</code>, <code>time_added</code> if there).</li>
          <li><b>Pocket Reader</b> — a JSON export from another box.</li>
        </ul>
        <div className="row wrap mt-s"><label className="btn primary">{busy ? <span className="spinner" /> : <Icon name="upload" size={15} />}{busy ? 'Importing…' : 'Choose a file…'}<input type="file" accept=".opml,.xml,.json,.html,.htm,.csv,text/*,application/json" hidden onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) importFile(f) }} /></label>{last && <span className="small ok">{last}</span>}</div>
      </Section>
      <Section title="Export" icon="download" sub="Everything of yours: feeds and folders, every article you kept (with its text), highlights and rules as JSON; your subscriptions as OPML for any other reader; your saved list as CSV; highlights as Markdown.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Everything (JSON)</a><a className="btn" href={`${V1}/export.opml`}><Icon name="rss" size={15} />Subscriptions (OPML)</a><a className="btn" href={`${V1}/export.csv`}><Icon name="bookmark" size={15} />Saved list (CSV)</a><a className="btn" href={`${V1}/highlights/export.md`}><Icon name="highlighter" size={15} />Highlights (Markdown)</a></div>
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-reader.db</code> in the data directory.</p>
        <a className="btn" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Feeds', counts.feeds, 'rss'], ['Articles', counts.articles, 'text'], ['Highlights', counts.highlights, 'highlighter'], ['Rules', counts.rules, 'sliders']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Reader</h3>
          <p className="muted small">Your feeds and your read-later list in one calm place: follow websites, blogs and YouTube channels, save any page as a clean article, highlight passages and keep them in your notes, search everything you read. Works with RSS phone apps. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <Link className="btn" to="/feeds"><Icon name="rss" size={15} />Feeds</Link>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-reader" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Notes keeps your highlights, Day writes what you read into your journal, Pocket AI summarises.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
