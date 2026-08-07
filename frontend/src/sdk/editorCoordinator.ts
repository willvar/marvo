type PrepareHandler = () => Promise<void>

const handlers = new Map<string, PrepareHandler>()

export function registerEditorPreparation(title: string, handler: PrepareHandler): () => void {
  handlers.set(title, handler)
  return () => {
    if (handlers.get(title) === handler) handlers.delete(title)
  }
}

export async function prepareNoteForAgent(title: string): Promise<void> {
  const handler = handlers.get(title)
  if (handler) await handler()
}
