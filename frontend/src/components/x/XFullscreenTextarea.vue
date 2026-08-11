<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { Dialog } from '@ark-ui/vue/dialog'
import { Field } from '@ark-ui/vue/field'
import { FullscreenExitOutlined, FullscreenOutlined } from '@ant-design/icons-vue'
import XButton from './XButton.vue'

defineOptions({ inheritAttrs: false })

withDefaults(
  defineProps<{
    title: string
    placeholder?: string
    disabled?: boolean
    readonly?: boolean
    expandLabel?: string
    collapseLabel?: string
  }>(),
  {
    placeholder: '',
    disabled: false,
    readonly: false,
    expandLabel: '展开编辑',
    collapseLabel: '退出全屏',
  },
)

const model = defineModel<string>({ required: true })
const expanded = ref(false)

function updateExpanded(value: boolean) {
  if (expanded.value === value) return
  expanded.value = value
  if (value) {
    window.addEventListener('keydown', closeExpandedFromEscape, { capture: true })
  } else {
    window.removeEventListener('keydown', closeExpandedFromEscape, { capture: true })
  }
}

function closeExpandedFromEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.isComposing) return
  event.preventDefault()
  event.stopImmediatePropagation()
  updateExpanded(false)
}

onBeforeUnmount(() => window.removeEventListener('keydown', closeExpandedFromEscape, { capture: true }))
</script>

<template>
  <div class="x-fullscreen-textarea">
    <Field.Textarea
      v-model="model"
      v-bind="$attrs"
      :placeholder="placeholder"
      :disabled="disabled"
      :readonly="readonly"
    />
    <div class="x-fullscreen-textarea-toolbar">
      <XButton variant="secondary" size="small" :disabled="disabled" @click="updateExpanded(true)">
        <FullscreenOutlined aria-hidden="true" />
        <span>{{ expandLabel }}</span>
      </XButton>
    </div>
  </div>

  <Dialog.Root :open="expanded" :close-on-escape="false" lazy-mount unmount-on-exit @update:open="updateExpanded">
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop x-fullscreen-textarea-backdrop" />
      <Dialog.Positioner class="dialog-positioner x-fullscreen-textarea-positioner">
        <Dialog.Content class="dialog-panel x-fullscreen-textarea-dialog">
          <div class="dialog-header x-fullscreen-textarea-header">
            <div>
              <Dialog.Title>{{ title }}</Dialog.Title>
              <Dialog.Description>内容会同步回原文本框，退出全屏不会提交表单。</Dialog.Description>
            </div>
            <Dialog.CloseTrigger as-child>
              <XButton variant="secondary" size="small">
                <FullscreenExitOutlined aria-hidden="true" />
                <span>{{ collapseLabel }}</span>
              </XButton>
            </Dialog.CloseTrigger>
          </div>

          <Field.Root class="x-fullscreen-textarea-field">
            <Field.Textarea
              v-model="model"
              class="x-fullscreen-textarea-editor"
              :placeholder="placeholder"
              :disabled="disabled"
              :readonly="readonly"
              :aria-label="title"
              autofocus
            />
          </Field.Root>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>
</template>

<style lang="scss">
.x-fullscreen-textarea {
  min-width: 0;
}
.x-fullscreen-textarea-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-top: 7px;
}
.x-fullscreen-textarea-backdrop,
.x-fullscreen-textarea-positioner {
  --dialog-z-index: 1400;
}
.x-fullscreen-textarea-positioner {
  align-items: stretch;
  padding: 0;
  overflow: hidden;
}
.x-fullscreen-textarea-dialog {
  width: 100vw;
  max-width: none;
  height: 100dvh;
  max-height: none;
  border: 0;
  border-radius: 0;
}
.x-fullscreen-textarea-header {
  flex: 0 0 auto;
  gap: 16px;
  padding-top: max(20px, env(safe-area-inset-top));
}
.x-fullscreen-textarea-header > div {
  min-width: 0;
}
.x-fullscreen-textarea-header [data-part='description'] {
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.x-fullscreen-textarea-field {
  display: flex;
  min-height: 0;
  flex: 1;
  padding: 0 24px max(24px, env(safe-area-inset-bottom));
}
.x-fullscreen-textarea-editor {
  width: 100%;
  min-height: 0;
  flex: 1;
  resize: none;
  padding: 18px 20px;
  border: 1px solid var(--border-light);
  border-radius: 10px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-14);
  line-height: 1.7;
}
.x-fullscreen-textarea-editor:focus {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}

@media (max-width: 600px) {
  .x-fullscreen-textarea-header {
    align-items: flex-start;
    padding-inline: 16px;
  }
  .x-fullscreen-textarea-field {
    padding-inline: 16px;
    padding-bottom: max(16px, env(safe-area-inset-bottom));
  }
  .x-fullscreen-textarea-editor {
    padding: 14px;
    font-size: var(--marvo-type-13);
  }
}
</style>
