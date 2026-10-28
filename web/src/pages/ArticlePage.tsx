// One article on its own page (phones, links from notifications and other apps).
import { useNavigate, useParams } from 'react-router-dom'
import { Reader } from '../components/Reader'

export default function ArticlePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const back = () => { if (window.history.length > 1) navigate(-1); else navigate('/') }
  return <div className="article-page"><Reader key={id} id={Number(id)} onBack={back} onGone={back} /></div>
}
