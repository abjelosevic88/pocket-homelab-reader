import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['1'], 'Unread'], [['2'], 'Read later'], [['3'], 'Starred'], [['4'], 'Highlights'], [['5'], 'History'], [['f'], 'Feeds'], [[','], 'Settings'], [['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Lists', keys: [[['j'], 'Next article'], [['k'], 'Previous article'], [['o'], 'Open'], [['⇧', 'A'], 'Mark all as read']] },
  { title: 'Article', keys: [[['m'], 'Read / unread'], [['s'], 'Star'], [['l'], 'Read later'], [['e'], 'Done (archive)'], [['v'], 'Open the original'], [['h'], 'Highlight the selection'], [['esc'], 'Back to the list']] },
  { title: 'Add', keys: [[['a'], 'Follow a website or save a page'], [['r'], 'Check all feeds now']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
    </Modal>
  )
}
