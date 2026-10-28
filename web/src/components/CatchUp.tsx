// "Catch me up": the AI reads the unread posts of a list and writes a short
// briefing by topic; the [ids] in it open the posts.
import React, { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { Article } from '../lib/types'
import { Icon } from './icons'
import { CoreHint, Loading, Modal } from './ui'

export function CatchUp({ feedId, folderId, onClose, onOpen }: { feedId?: number; folderId?: number; onClose: () => void; onOpen: (id: number) => void }) {
  const [res, setRes] = useState<{ ok: boolean; text?: string; error?: string; count?: number; posts?: Article[] } | null>(null)
  useEffect(() => {
    api.post<typeof res>(`${V1}/ai/catch-up`, { feed_id: feedId || 0, folder_id: folderId || 0 }).then(setRes).catch(e => setRes({ ok: false, error: (e as Error).message }))
  }, [feedId, folderId])
  const titles = new Map((res?.posts || []).map(p => [p.id, p.title]))
  const line = (l: string, i: number) => {
    const parts: React.ReactNode[] = []
    let last = 0
    for (const m of l.matchAll(/\[([\d,\s]+)\]/g)) {
      parts.push(l.slice(last, m.index))
      const ids = m[1].split(',').map(s => Number(s.trim())).filter(n => titles.has(n))
      parts.push(<span key={m.index} className="cu-refs">{ids.map(id => <button key={id} type="button" className="chip" title={titles.get(id)} onClick={() => { onClose(); onOpen(id) }}>{(titles.get(id) || '').slice(0, 38)}{(titles.get(id) || '').length > 38 ? '…' : ''}</button>)}</span>)
      last = (m.index || 0) + m[0].length
    }
    parts.push(l.slice(last))
    return <p key={i}>{parts}</p>
  }
  return (
    <Modal title={<span className="row"><Icon name="sparkles" size={16} />Catch me up</span>} onClose={onClose} size="lg">
      {!res ? <Loading label="Reading your unread posts…" />
        : !res.ok ? <div className="callout info"><Icon name="info" size={15} /><div>{res.error}{/Pocket|AI|key/i.test(res.error || '') && <CoreHint />}</div></div>
          : <div className="catchup">
            <p className="muted small">{res.count} unread posts, newest first. The briefing is written by AI from the start of each post; open a post to read it in full.</p>
            {(res.text || '').split('\n').filter(l => l.trim()).map((l, i) => line(l.replace(/^[-•#*]+\s*/, '').replace(/\*\*/g, ''), i))}
          </div>}
    </Modal>
  )
}
