// /save?url=…&title=… — where the bookmarklet, the phone's share sheet and
// shortcuts land: the page is saved at once; following its site is one tap.
import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announce, hostOf } from '../lib/reader'
import type { Article } from '../lib/types'
import { Icon } from '../components/icons'
import { Spinner } from '../components/ui'

export default function Save() {
  const [sp] = useSearchParams()
  const navigate = useNavigate()
  const raw = sp.get('url') || sp.get('text') || ''
  const url = (raw.match(/https?:\/\/\S+/) || [raw])[0]
  const [state, setState] = useState<{ article?: Article; created?: boolean; error?: string }>({})
  const once = useRef(false)
  useEffect(() => {
    if (once.current || !url) return
    once.current = true
    api.post<{ article: Article; created: boolean }>(`${V1}/saved?source=${sp.get('source') === 'share' ? 'share' : 'bookmarklet'}`, { url, title: sp.get('title') || '' })
      .then(r => { setState(r); announce() }).catch(e => setState({ error: (e as Error).message }))
  }, [url, sp])
  const popup = window.opener !== null && window.name === 'pocketreader'
  useEffect(() => { if (popup && state.article) { const t = window.setTimeout(() => window.close(), 2500); return () => window.clearTimeout(t) } }, [popup, state.article])
  return (
    <div className="save-page">
      <div className="save-card">
        <img src="/favicon.svg" alt="" width={40} height={40} />
        {!url ? <><h1>Nothing to save</h1><p className="muted">Open this from the bookmarklet or your phone’s share sheet.</p></>
          : state.error ? <><h1>Could not save</h1><p className="bad">{state.error}</p><p className="muted small ellipsis">{url}</p></>
            : !state.article ? <><h1 className="row"><Spinner />Saving…</h1><p className="muted small ellipsis">{hostOf(url)}</p></>
              : <>
                <h1 className="row"><Icon name="bookmark-check" size={22} className="ok" />{state.created ? 'Saved to read later' : 'Already saved — moved to the top'}</h1>
                <p className="save-title">{state.article.title}</p>
                <p className="muted small">{state.article.site_name || hostOf(url)}{state.article.extract_state === 'done' ? ` · ${Math.max(1, Math.round(state.article.words / 230))} min read` : ''}</p>
                <div className="row wrap mt">
                  {popup ? <button type="button" className="btn" onClick={() => window.close()}>Close</button> : <button type="button" className="btn primary" onClick={() => navigate(`/a/${state.article!.id}`)}>Read now</button>}
                  <Link className="btn" to={`/?add=${encodeURIComponent(url)}`} target={popup ? '_blank' : undefined}><Icon name="rss" size={15} />Follow this site</Link>
                  {!popup && <Link className="btn ghost" to="/saved">Read later list</Link>}
                </div>
              </>}
      </div>
    </div>
  )
}
