export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  font: 'serif' | 'sans' | 'mono'; font_size: number; line_width: 'narrow' | 'medium' | 'wide'; line_height: 'tight' | 'normal' | 'loose'
  reader_theme: 'app' | 'paper' | 'sepia' | 'night'; justify: boolean
  mark_on_open: boolean; mark_on_scroll: boolean; sort_oldest: boolean; list_style: 'cards' | 'compact' | 'magazine'; show_images: boolean
  open_original: boolean; hide_read: boolean; confirm_all: boolean; auto_extract: boolean
  refresh_min: number; keep_read_days: number; keep_unread_days: number
  notify_url: string; notify_format: string; notify_configured: boolean
  notify_new_posts: boolean; digest_enabled: boolean; digest_hour: number; digest_days: string; notify_feed_errors: boolean
  notes_mode: ConnMode; notes_via: ConnVia; notes_detail: string; notes_open_url: string; notes_configured: boolean
  notes_url: string; notes_token: string; notes_public_url: string; notes_folder: string; notes_auto: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  ai_auto_tag: boolean; ai_language: string
  setup_complete: boolean; base_url: string; fetch_private: boolean
}

export interface Folder { id: number; name: string; sort: number; unread: number }

export interface Feed {
  id: number; folder_id: number | null; url: string; site_url: string; title: string; custom_title: string; description: string; icon_url: string
  kind: 'web' | 'youtube' | 'podcast'; full_text: boolean; notify: boolean; paused: boolean; refresh_min: number
  fetched_at: string; next_fetch_at: string; newest_at: string; error: string; error_count: number; source: string; created_at: string
  unread: number; total: number
}

export interface Article {
  id: number; feed_id: number | null; url: string; title: string; author: string; site_name: string; excerpt: string; content?: string
  image_url: string; media_url: string; media_type: string; lang: string; words: number; published_at: string; added_at: string
  read: boolean; read_at: string; starred: boolean; starred_at: string; saved: boolean; saved_at: string; archived: boolean; archived_at: string
  progress: number; extract_state: '' | 'pending' | 'done' | 'failed'; extract_error: string; tags: string; note: string; ai_summary: string
  notes_note_id: number; notes_synced_at: string; source: string; feed_title: string; feed_icon: string; highlights: number
}

export interface ArticleView extends Article { minutes: number; feed?: Feed; highlight_list: Highlight[] }

export interface Highlight {
  id: number; article_id: number; text: string; note: string; color: 'yellow' | 'green' | 'blue' | 'pink'; prefix: string; suffix: string; position: number
  created_at: string; updated_at: string; article_title?: string; article_url?: string; site_name?: string
}

export interface Rule {
  id: number; name: string; feed_id: number | null; folder_id: number | null; field: 'title' | 'any' | 'author' | 'url'; words: string
  action: 'read' | 'star' | 'save' | 'tag' | 'notify'; tag: string; enabled: boolean; hits: number; last_hit_at: string; created_at: string
}

export interface Counts {
  unread: number; today: number; saved: number; starred: number; highlights: number; feeds: number; failing: number
  read_today: number; saved_today: number; read_minutes_today: number
}

export interface Page { items: Article[]; next: string }

export interface Discovery {
  url: string; is_feed: boolean
  feeds: { url: string; title: string; kind: string; feed_id: number }[]
  page?: { title: string; excerpt: string; image_url: string; site_name: string; saved_id?: number }
  preview?: { title: string; published_at: string }[]
}
