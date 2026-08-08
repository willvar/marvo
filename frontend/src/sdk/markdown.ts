import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import { toNoteAssetUrl } from './utils/noteAssets'

const md = new MarkdownIt({ html: true, breaks: true, linkify: true })

interface MarkdownRenderOptions {
  title?: string
  openLinksInNewTab?: boolean
}

export function renderMarkdown(content: string, options: MarkdownRenderOptions = {}): string {
  if (!content) return ''
  const rendered = md.render(content)
  const sanitized = String(
    DOMPurify.sanitize(rendered, {
      ADD_TAGS: ['video', 'source'],
      ADD_ATTR: ['controls', 'preload', 'playsinline', 'poster'],
      FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form', 'input', 'button', 'textarea', 'select'],
      FORBID_ATTR: ['style'],
    }),
  )

  const root = document.createElement('div')
  root.innerHTML = sanitized
  if (options.title) resolveAssetUrls(root, options.title)
  renderAssetPlaceholders(root)
  root.querySelectorAll('pre').forEach((element) => element.setAttribute('data-scrollable', ''))
  root.querySelectorAll<HTMLAnchorElement>('a[href]').forEach((link) => {
    link.rel = 'noopener noreferrer'
    if (options.openLinksInNewTab) link.target = '_blank'
  })
  return root.innerHTML
}

function renderAssetPlaceholders(root: HTMLElement) {
  root.querySelectorAll<HTMLElement>('[data-marvo-asset-id]').forEach((placeholder) => {
    placeholder.classList.add('marvo-asset-placeholder', 'state-reserved')
    const name = placeholder.dataset.marvoAssetName || '媒体文件'
    placeholder.textContent = `${name} · 媒体仍在处理或等待重新上传`
  })
}

function resolveAssetUrls(root: HTMLElement, title: string) {
  root.querySelectorAll<HTMLElement>('[src], [href], [poster]').forEach((element) => {
    for (const attr of ['src', 'href', 'poster']) {
      const value = element.getAttribute(attr)
      if (value) element.setAttribute(attr, toNoteAssetUrl(value, title))
    }
  })
}
