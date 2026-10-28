// Every passage you marked, newest first, grouped by article.
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { hostOf, since } from '../lib/reader'
import type { Highlight } from '../lib/types'
import { Icon } from '../components/icons'
import { Empty, Loading, PageHeader, useAsync, useDebounce } from '../components/ui'

export default function Highlights() {
  const [q, setQ] = useState('')
  const dq = useDebounce(q.trim(), 250)
  const { data } = useAsync(() => api.get<Highlight[]>(`${V1}/highlights?limit=500${dq ? `&q=${encodeURIComponent(dq)}` : ''}`), [dq])
  const groups = useMemo(() => {
    const out: { id: number; title: string; url: string; site: string; items: Highlight[] }[] = []
    for (const h of data || []) {
      let g = out.find(x => x.id === h.article_id)
      if (!g) { g = { id: h.article_id, title: h.article_title || 'Untitled', url: h.article_url || '', site: h.site_name || hostOf(h.article_url || ''), items: [] }; out.push(g) }
      g.items.push(h)
    }
    return out
  }, [data])
  return (
    <div className="hl-page">
      <PageHeader title="Highlights" sub={data ? `${data.length} passages from ${groups.length} articles` : undefined}
        actions={<a className="btn" href={`${V1}/highlights/export.md`}><Icon name="download" size={15} />Export as Markdown</a>} />
      <div className="hl-search"><Icon name="search" size={15} className="muted" /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search your highlights and notes" aria-label="Search highlights" /></div>
      {!data ? <Loading /> : groups.length === 0 ? <Empty title={dq ? 'No highlights match' : 'No highlights yet'} icon="highlighter">{dq ? 'Try other words.' : 'Select text in any article and press Highlight. Highlights can go to Pocket Notes as a note per article.'}</Empty> : (
        <div className="hl-groups">
          {groups.map(g => (
            <section key={g.id} className="hl-group">
              <div className="hl-group-head">
                <Link to={`/a/${g.id}`} className="hl-group-title">{g.title}</Link>
                <span className="muted small">{g.site} · {since(g.items[0].created_at)}</span>
              </div>
              {g.items.map(h => (
                <Link key={h.id} to={`/a/${g.id}`} className={`hl-card hl-c-${h.color} as-link`}>
                  <blockquote>{h.text}</blockquote>
                  {h.note && <p className="hl-note">{h.note}</p>}
                </Link>
              ))}
            </section>
          ))}
        </div>
      )}
    </div>
  )
}
