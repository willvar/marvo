import MarkdownIt from 'markdown-it'
import { Image } from 'antd'
import React, { useMemo } from 'react'

const md = new MarkdownIt({
  html: true,
  breaks: true,
  linkify: true,
})

export default function NotePreview({ content }: { content: string }) {
  const rendered = useMemo(() => renderMarkdown(content), [content])

  if (!content) {
    return <div className="editor-content preview-empty">暂无内容</div>
  }

  return (
    <div className="editor-content note-preview">
      <div className="tiptap">
        {rendered}
      </div>
    </div>
  )
}

function renderMarkdown(content: string) {
  const html = md.render(content)
  if (typeof DOMParser === 'undefined') {
    return null
  }

  const document = new DOMParser().parseFromString(html, 'text/html')
  return Array.from(document.body.childNodes).map((node, index) => renderNode(node, `node-${index}`))
}

function renderNode(node: Node, key: string, insideParagraph = false): React.ReactNode {
  if (node.nodeType === Node.TEXT_NODE) {
    return node.textContent
  }

  if (!(node instanceof HTMLElement)) {
    return null
  }

  const tagName = node.tagName.toLowerCase()
  if (tagName === 'script' || tagName === 'style') {
    return null
  }

  if (tagName === 'p' && isImageOnlyParagraph(node)) {
    const children = Array.from(node.childNodes).map((child, index) => renderNode(child, `${key}-${index}`))
    return React.createElement('div', { key, className: 'image-paragraph' }, children)
  }

  if (tagName === 'p') {
    const children = Array.from(node.childNodes).map((child, index) => renderNode(child, `${key}-${index}`, true))
    return React.createElement('p', { key, ...elementProps(node) }, children)
  }

  if (tagName === 'img') {
    if (insideParagraph) {
      return React.createElement('img', {
        key,
        src: node.getAttribute('src') || '',
        alt: node.getAttribute('alt') || '',
      })
    }

    return (
      <Image
        key={key}
        src={node.getAttribute('src') || ''}
        alt={node.getAttribute('alt') || ''}
        preview={{ cover: false }}
      />
    )
  }

  const children = Array.from(node.childNodes).map((child, index) => renderNode(child, `${key}-${index}`))
  return React.createElement(tagName, { key, ...elementProps(node) }, children)
}

function isImageOnlyParagraph(element: HTMLElement) {
  return Array.from(element.childNodes).every(node => {
    if (node.nodeType === Node.TEXT_NODE) {
      return !node.textContent?.trim()
    }
    return node instanceof HTMLImageElement
  })
}

function elementProps(element: HTMLElement) {
  const props: Record<string, string> = {}

  for (const attr of Array.from(element.attributes)) {
    if (attr.name.startsWith('on')) continue
    if (attr.name === 'class') {
      props.className = attr.value
      continue
    }
    if (attr.name === 'style') continue
    props[attr.name] = attr.value
  }

  return props
}
