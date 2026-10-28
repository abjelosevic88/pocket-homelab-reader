// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Counts, Feed, Folder } from '../lib/types'
import { announce } from '../lib/reader'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { AddDialog } from './AddDialog'
import { Avatar, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery, useToast } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'prd-sidebar'

/** Something was added or changed (a post read, a feed followed): counts and lists reload. */
export const announceChange = announce
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('rd:changed', h)
    return () => window.removeEventListener('rd:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** Feeds, folders and the sidebar numbers, for every page; reload() after a change. */
interface FeedsState { feeds: Feed[]; folders: Folder[]; counts: Counts | null; loaded: boolean; reload: () => void }
const FeedsCtx = createContext<FeedsState>({ feeds: [], folders: [], counts: null, loaded: false, reload: () => {} })
export const useFeeds = () => useContext(FeedsCtx)

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const { toast } = useToast()
  const [counts, setCounts] = useState<Counts | null>(null)
  const [data, setData] = useState<{ feeds: Feed[]; folders: Folder[] }>({ feeds: [], folders: [] })
  const [loaded, setLoaded] = useState(false)
  const load = useCallback(() => {
    api.get<Counts>(`${V1}/dashboard`).then(setCounts).catch(() => {})
    api.get<{ feeds: Feed[]; folders: Folder[] }>(`${V1}/feeds`).then(d => { setData(d); setLoaded(true) }).catch(() => {})
  }, [])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  // new posts arrive in the background: refresh the numbers every two minutes while the tab is open
  useEffect(() => {
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') load() }, 120_000)
    const vis = () => { if (document.visibilityState === 'visible') load() }
    document.addEventListener('visibilitychange', vis)
    return () => { window.clearInterval(t); document.removeEventListener('visibilitychange', vis) }
  }, [load])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [add, setAdd] = useState<{ url: string } | null>(null)

  // /?add=<url> (the PWA shortcut, a share) opens the add dialog
  useEffect(() => { const u = sp.get('add'); if (u !== null) { setAdd({ url: u === '1' ? '' : u }); setSp(new URLSearchParams(), { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const refreshAll = async () => {
    try { await api.post(`${V1}/feeds/refresh`); toast('Checking your feeds…'); window.setTimeout(load, 6000); window.setTimeout(load, 20000) } catch (e) { toast((e as Error).message, 'err') }
  }
  const refreshRef = useRef(refreshAll)
  refreshRef.current = refreshAll

  useEffect(() => {
    const open = () => setShortcuts(true)
    const mk = (e: Event) => { const d = (e as CustomEvent<{ url: string }>).detail; closeDrawer(); setAdd({ url: d?.url || '' }) }
    window.addEventListener('rd:shortcuts', open)
    window.addEventListener('rd:add', mk)
    return () => { window.removeEventListener('rd:shortcuts', open); window.removeEventListener('rd:add', mk) }
  }) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'a': e.preventDefault(); setAdd({ url: '' }); break
        case 'r': e.preventDefault(); refreshRef.current(); break
        case '1': go('/'); break
        case '2': go('/saved'); break
        case '3': go('/starred'); break
        case '4': go('/highlights'); break
        case '5': go('/history'); break
        case 'f': if (!e.shiftKey) go('/feeds'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'add', label: 'Follow a website or save a page…', icon: 'plus', hint: 'A', keywords: 'add subscribe feed rss url link youtube', run: () => setAdd({ url: '' }) },
    { id: 'refresh', label: 'Check all feeds now', icon: 'refresh', hint: 'R', keywords: 'refresh update fetch', run: refreshAll },
    { id: 'unread', label: 'Unread', icon: 'inbox', hint: '1', run: () => navigate('/') },
    { id: 'saved', label: 'Read later', icon: 'bookmark', hint: '2', keywords: 'saved pocket instapaper', run: () => navigate('/saved') },
    { id: 'starred', label: 'Starred', icon: 'star', hint: '3', run: () => navigate('/starred') },
    { id: 'highlights', label: 'Highlights', icon: 'highlighter', hint: '4', keywords: 'notes quotes', run: () => navigate('/highlights') },
    { id: 'history', label: 'History', icon: 'clock', hint: '5', keywords: 'read recently', run: () => navigate('/history') },
    { id: 'archive', label: 'Archive', icon: 'archive', keywords: 'finished done', run: () => navigate('/archive') },
    { id: 'all', label: 'Everything', icon: 'layers', run: () => navigate('/all') },
    { id: 'feeds', label: 'Manage feeds and folders', icon: 'rss', hint: 'F', keywords: 'subscriptions opml folders', run: () => navigate('/feeds') },
    { id: 'rules', label: 'Rules', icon: 'sliders', keywords: 'filter mute keyword auto tag', run: () => navigate('/feeds/rules') },
    { id: 'import', label: 'Import from FreshRSS, Karakeep, bookmarks…', icon: 'upload', keywords: 'opml import csv pocket instapaper', run: () => navigate('/settings/data') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'reading', label: 'Settings: reading', icon: 'text', keywords: 'font size theme serif width', run: () => navigate('/settings/reading') },
    { id: 'phone', label: 'Settings: phone apps and bookmarklet', icon: 'smartphone', keywords: 'reeder netnewswire google reader api token shortcut', run: () => navigate('/settings/tokens') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  const feedsState: FeedsState = { feeds: data.feeds, folders: data.folders, counts, loaded, reload: load }
  return (
    <CrumbCtx.Provider value={setCrumb}>
      <FeedsCtx.Provider value={feedsState}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onAdd={() => { closeDrawer(); setAdd({ url: '' }) }} onRefresh={refreshAll} onNavigate={closeDrawer} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && !/^\/a\//.test(loc.pathname) && <button type="button" className="fab" aria-label="Add" title="Follow a website or save a page" onClick={() => setAdd({ url: '' })}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} feeds={data.feeds} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {add && <AddDialog initial={add.url} onClose={() => setAdd(null)} onDone={() => { setAdd(null); load() }} />}
        </div>
      </FeedsCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Unread', '/'], all: ['Everything', '/all'], today: ['Today', '/today'], saved: ['Read later', '/saved'], archive: ['Archive', '/archive'], starred: ['Starred', '/starred'], history: ['History', '/history'], highlights: ['Highlights', '/highlights'], feeds: ['Feeds', '/feeds'], feed: ['Feeds', '/feeds'], folder: ['Feeds', '/feeds'], tag: ['Tags', '/saved'], search: ['Search', '/'], a: ['Article', '/'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('rd:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
