// Shared bits of Pocket Reader: list views, article actions, small formatters.
import { api, V1 } from './api'
import type { Article, Feed, Folder } from './types'

export type View = 'unread' | 'all' | 'today' | 'saved' | 'archive' | 'starred' | 'history' | 'highlights'

/** What a list shows: a view, a feed, a folder, a tag or a search. */
export interface ListSpec { view: View; feed?: number; folder?: number; tag?: string; q?: string }

export const VIEW_TITLE: Record<View, string> = {
  unread: 'Unread', all: 'Everything', today: 'Today', saved: 'Read later', archive: 'Archive', starred: 'Starred', history: 'History', highlights: 'Highlighted',
}

export const VIEW_EMPTY: Record<View, [string, string]> = {
  unread: ['All caught up', 'Nothing unread. New posts arrive as your feeds publish them.'],
  all: ['Nothing here yet', 'Follow a website or save a page to read later.'],
  today: ['Quiet day', 'Nothing new since yesterday.'],
  saved: ['Your read-later list is empty', 'Save any page with the + button, the bookmarklet or your phone’s share sheet.'],
  archive: ['No finished articles', 'Articles you finish from your read-later list land here.'],
  starred: ['Nothing starred', 'Star a post to keep it; starred posts are never cleaned up.'],
  history: ['Nothing read yet', 'What you open shows up here, newest first.'],
  highlights: ['No highlights', 'Select text in an article to highlight it.'],
}

export function listQuery(s: ListSpec): string {
  const p = new URLSearchParams()
  if (s.feed || s.folder) {
    if (s.feed) p.set('feed', String(s.feed))
    if (s.folder) p.set('folder', String(s.folder))
    if (s.view !== 'all') p.set('view', s.view)
  } else if (s.q) {
    p.set('q', s.q)
  } else {
    p.set('view', s.view)
  }
  if (s.tag) p.set('tag', s.tag)
  return p.toString()
}

export const setState = (id: number, patch: Partial<Pick<Article, 'read' | 'starred' | 'saved' | 'archived' | 'progress' | 'tags' | 'note' | 'title'>>) =>
  api.put<Article>(`${V1}/articles/${id}`, patch)

export const feedName = (f?: Pick<Feed, 'custom_title' | 'title' | 'url'> | null) => f ? (f.custom_title || f.title || hostOf(f.url)) : ''

export function hostOf(url: string): string {
  try { return new URL(url).hostname.replace(/^www\./, '') } catch { return url }
}

/** "5 min" reading time. */
export const readTime = (words: number) => `${Math.max(1, Math.round(words / 230))} min`

/** Short relative time: "now", "5m", "3h", "2d", or a date. */
export function since(iso: string): string {
  const t = Date.parse(iso)
  if (!t) return ''
  const s = (Date.now() - t) / 1000
  if (s < 60) return 'now'
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  if (s < 7 * 86400) return `${Math.floor(s / 86400)}d`
  const d = new Date(t)
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', ...(d.getFullYear() !== new Date().getFullYear() ? { year: 'numeric' } : {}) })
}

export const splitTags = (s: string) => s.split(',').map(t => t.trim()).filter(Boolean)

/** Folders with their feeds, plus the feeds outside any folder. */
export function groupFeeds(feeds: Feed[], folders: Folder[]) {
  const groups = folders.map(f => ({ folder: f, feeds: feeds.filter(x => x.folder_id === f.id) }))
  const loose = feeds.filter(x => !x.folder_id || !folders.some(f => f.id === x.folder_id))
  return { groups, loose }
}

/** Something changed (an article read, a feed added): counts and lists reload. */
export const announce = () => window.dispatchEvent(new CustomEvent('rd:changed'))

/** Open the add dialog from anywhere, optionally with an address filled in. */
export const openAdd = (url = '') => window.dispatchEvent(new CustomEvent('rd:add', { detail: { url } }))

/** The bookmarklet: saves the page you are on, or opens Reader to follow its feed. */
export const bookmarklet = (base: string) =>
  `javascript:(function(){window.open('${base}/save?url='+encodeURIComponent(location.href)+'&title='+encodeURIComponent(document.title),'pocketreader','width=520,height=640')})()`
