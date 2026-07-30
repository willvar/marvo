import { Extension } from '@tiptap/core'
import { collab } from 'prosemirror-collab'

interface OTCollabOptions {
  version: number
  clientID: string
}

export const OTCollab = Extension.create<OTCollabOptions>({
  name: 'otCollab',

  addOptions() {
    return {
      version: 0,
      clientID: '',
    }
  },

  addProseMirrorPlugins() {
    return [collab({ version: this.options.version, clientID: this.options.clientID })]
  },
})
