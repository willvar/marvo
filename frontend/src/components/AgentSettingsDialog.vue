<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Dialog } from '@ark-ui/vue/dialog'
import { Combobox, useListCollection } from '@ark-ui/vue/combobox'
import { Field } from '@ark-ui/vue/field'
import { RadioGroup } from '@ark-ui/vue/radio-group'
import { SegmentGroup } from '@ark-ui/vue/segment-group'
import { Tabs } from '@ark-ui/vue/tabs'
import {
  CheckOutlined,
  CloseOutlined,
  ControlOutlined,
  ApiOutlined,
  DeleteOutlined,
  DownOutlined,
  LayoutOutlined,
  MessageOutlined,
  PlusOutlined,
  RobotOutlined,
  SaveOutlined,
} from '@ant-design/icons-vue'
import { v4 as uuidv4 } from 'uuid'
import { useAgentPersonalizationStore } from '../stores/agentPersonalization'
import { useAgentSettingsStore } from '../stores/agentSettings'
import { useUIPreferencesStore, type AgentAssistantDisplayMode } from '../stores/uiPreferences'
import type { AgentModelOption, AgentModelSelection, AgentPersonalizationRule } from '../sdk'
import AgentProviderSettings from './AgentProviderSettings.vue'
import { XFullscreenTextarea } from './x'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [open: boolean] }>()

const MAX_PROMPT_BYTES = 64 * 1024
const MAX_PERSONALIZATION_RULE_BYTES = 4 * 1024
const MAX_PERSONALIZATION_RULES = 256
const DEFAULT_VARIANT = '__model_default__'
type AgentSettingsTab = 'style' | 'providers' | 'model' | 'advanced'
const TAB_LABELS: Record<AgentSettingsTab, string> = {
  style: '样式',
  providers: '提供商',
  model: '模型',
  advanced: '进阶',
}
const settingsStore = useAgentSettingsStore()
const personalizationStore = useAgentPersonalizationStore()
const uiPreferences = useUIPreferencesStore()
const activeTab = ref<AgentSettingsTab>('style')
const tabChangeOpen = ref(false)
const tabChangeSourceTab = ref<AgentSettingsTab | null>(null)
const pendingTab = ref<AgentSettingsTab | null>(null)
const displayMode = ref<AgentAssistantDisplayMode>('floating')
const styleSnapshot = ref<AgentAssistantDisplayMode>('floating')
const loading = ref(false)
const saving = ref(false)
const settingsReady = ref(false)
const modelSnapshot = ref('')
const promptSnapshot = ref('')
const error = ref('')
const models = ref<AgentModelOption[]>([])
const selectedValues = ref<string[]>([])
const selectedVariant = ref(DEFAULT_VARIANT)
const globalPrompt = ref('')
const unavailableModel = ref<AgentModelSelection | null>(null)
const unavailableVariant = ref('')
const personalizationRules = ref<AgentPersonalizationRule[]>([])
const personalizationRevision = ref('')
const personalizationSnapshot = ref('')
const personalizationReady = ref(false)
const personalizationLoading = ref(false)
const personalizationError = ref('')
const touchedPersonalizationRuleIDs = ref<Set<string>>(new Set())
let loadSequence = 0
let personalizationLoadSequence = 0

const { collection, filter, set } = useListCollection<AgentModelOption>({
  initialItems: [],
  itemToValue: modelKey,
  itemToString: (model) => `${model.name} · ${model.provider_name}`,
  filter: (_itemText, query, model) => {
    const haystack = [model.name, model.model_id, model.provider_name, model.provider_id, model.family]
      .filter(Boolean)
      .join(' ')
      .toLocaleLowerCase()
    return haystack.includes(query.trim().toLocaleLowerCase())
  },
})

const selectedModel = computed(() => {
  const key = selectedValues.value[0]
  return key ? models.value.find((model) => modelKey(model) === key) || null : null
})
const variantOptions = computed(() => [
  { value: DEFAULT_VARIANT, label: '模型默认' },
  ...(selectedModel.value?.variants || []).map((value) => ({ value, label: variantLabel(value) })),
])
const promptBytes = computed(() => new TextEncoder().encode(globalPrompt.value).byteLength)
const promptTooLarge = computed(() => promptBytes.value > MAX_PROMPT_BYTES)
const styleDirty = computed(() => displayMode.value !== styleSnapshot.value)
const modelDirty = computed(() => settingsReady.value && modelSnapshot.value !== modelDraftSnapshot())
const promptDirty = computed(() => settingsReady.value && promptSnapshot.value !== globalPrompt.value)
const personalizationDirty = computed(
  () => personalizationReady.value && personalizationSnapshot.value !== personalizationDraftSnapshot(),
)
const advancedDirty = computed(() => promptDirty.value || personalizationDirty.value)
const personalizationInvalid = computed(() => {
  const texts = new Set<string>()
  for (const rule of personalizationRules.value) {
    const text = rule.text.trim()
    if (!text || new TextEncoder().encode(text).byteLength > MAX_PERSONALIZATION_RULE_BYTES || texts.has(text))
      return true
    texts.add(text)
  }
  return false
})
const currentTabDirty = computed(() => {
  if (activeTab.value === 'style') return styleDirty.value
  if (activeTab.value === 'model') return modelDirty.value
  if (activeTab.value === 'advanced') return advancedDirty.value
  return false
})
const canSave = computed(() => !saving.value && currentTabDirty.value)

watch(
  selectedModel,
  (model) => {
    unavailableVariant.value = ''
    if (selectedVariant.value !== DEFAULT_VARIANT && !model?.variants.includes(selectedVariant.value)) {
      selectedVariant.value = DEFAULT_VARIANT
    }
  },
  { flush: 'sync' },
)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    activeTab.value = 'style'
    tabChangeOpen.value = false
    tabChangeSourceTab.value = null
    pendingTab.value = null
    displayMode.value = uiPreferences.agentAssistantDisplayMode
    styleSnapshot.value = displayMode.value
    touchedPersonalizationRuleIDs.value = new Set()
    void loadSettings()
    void loadPersonalization()
  },
  { immediate: true },
)

function modelKey(model: Pick<AgentModelOption, 'provider_id' | 'model_id'>) {
  return JSON.stringify([model.provider_id, model.model_id])
}

async function loadPersonalization() {
  const sequence = ++personalizationLoadSequence
  personalizationLoading.value = true
  personalizationReady.value = false
  personalizationError.value = ''
  try {
    const snapshot = await personalizationStore.load(true)
    if (sequence !== personalizationLoadSequence || !props.open) return
    personalizationRules.value = snapshot.rules.map((rule) => ({ ...rule }))
    personalizationRevision.value = snapshot.revision
    touchedPersonalizationRuleIDs.value = new Set()
    personalizationReady.value = true
    personalizationSnapshot.value = personalizationDraftSnapshot()
  } catch (cause) {
    if (sequence !== personalizationLoadSequence || !props.open) return
    personalizationError.value = cause instanceof Error ? cause.message : '读取个性化规则失败'
  } finally {
    if (sequence === personalizationLoadSequence) personalizationLoading.value = false
  }
}

async function loadSettings() {
  const sequence = ++loadSequence
  loading.value = true
  settingsReady.value = false
  modelSnapshot.value = ''
  promptSnapshot.value = ''
  error.value = ''
  unavailableModel.value = null
  unavailableVariant.value = ''
  try {
    const settings = await settingsStore.load(true)
    if (sequence !== loadSequence || !props.open) return
    models.value = Array.isArray(settings.models)
      ? settings.models.map((model) => ({ ...model, variants: Array.isArray(model.variants) ? model.variants : [] }))
      : []
    set(models.value)
    globalPrompt.value = settings.global_prompt || ''
    if (settings.model && settings.model_available) {
      selectedValues.value = [modelKey(settings.model)]
      const model = models.value.find((candidate) => modelKey(candidate) === modelKey(settings.model!))
      if (settings.variant && model?.variants.includes(settings.variant)) {
        selectedVariant.value = settings.variant
      } else {
        selectedVariant.value = DEFAULT_VARIANT
        unavailableVariant.value = settings.variant || ''
      }
    } else {
      selectedValues.value = []
      selectedVariant.value = DEFAULT_VARIANT
      unavailableModel.value = settings.model
    }
    if (models.value.length === 0) error.value = 'OpenCode 当前没有已连接且可选择的模型。'
    settingsReady.value = true
    modelSnapshot.value = modelDraftSnapshot()
    promptSnapshot.value = globalPrompt.value
  } catch (cause) {
    if (sequence !== loadSequence || !props.open) return
    error.value = cause instanceof Error ? cause.message : '读取智能体设置失败'
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

async function refreshModels() {
  try {
    const settings = await settingsStore.load(true)
    const refreshed = Array.isArray(settings.models)
      ? settings.models.map((model) => ({ ...model, variants: Array.isArray(model.variants) ? model.variants : [] }))
      : []
    const currentKey = selectedValues.value[0] || ''
    models.value = refreshed
    set(refreshed)
    if (currentKey && !refreshed.some((model) => modelKey(model) === currentKey)) {
      selectedValues.value = settings.model_available && settings.model ? [modelKey(settings.model)] : []
      selectedVariant.value = DEFAULT_VARIANT
    }
    if (refreshed.length > 0 && error.value === 'OpenCode 当前没有已连接且可选择的模型。') error.value = ''
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '刷新智能体模型失败'
  }
}

async function saveSettings() {
  await saveCurrentTab(true)
}

async function saveCurrentTab(closeOnSuccess: boolean) {
  if (!canSave.value) return false
  const tab = activeTab.value
  if (!validateCurrentTab(tab)) return false
  let savePhase: 'personalization' | 'settings' | 'style' = tab === 'style' ? 'style' : 'settings'
  saving.value = true
  error.value = ''
  personalizationError.value = ''
  try {
    if (tab === 'style') {
      uiPreferences.setAgentAssistantDisplayMode(displayMode.value)
      styleSnapshot.value = displayMode.value
    } else if (tab === 'model') {
      savePhase = 'settings'
      await saveAgentSettings()
    } else if (tab === 'advanced') {
      if (personalizationDirty.value) {
        savePhase = 'personalization'
        const snapshot = await personalizationStore.save(
          personalizationRules.value.map((rule) => ({ ...rule, text: rule.text.trim() })),
          personalizationRevision.value,
        )
        personalizationRules.value = snapshot.rules.map((rule) => ({ ...rule }))
        personalizationRevision.value = snapshot.revision
        personalizationSnapshot.value = personalizationDraftSnapshot()
      }
      if (promptDirty.value) {
        savePhase = 'settings'
        await saveAgentSettings()
      }
    }
    if (closeOnSuccess) emit('update:open', false)
    return true
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : '保存智能体设置失败'
    if (savePhase === 'personalization') {
      personalizationError.value = message
    } else {
      error.value = message
    }
    return false
  } finally {
    saving.value = false
  }
}

function validateCurrentTab(tab: AgentSettingsTab) {
  if (tab === 'model' && (!settingsReady.value || !selectedModel.value || loading.value)) {
    error.value = '请选择智能体模型'
    return false
  }
  if (tab !== 'advanced') return true
  if (promptTooLarge.value) return false
  if (personalizationDirty.value && personalizationInvalid.value) {
    touchedPersonalizationRuleIDs.value = new Set(personalizationRules.value.map((rule) => rule.id))
    return false
  }
  if (promptDirty.value && (!settingsReady.value || !selectedModel.value || loading.value)) {
    error.value = '请先选择智能体模型'
    return false
  }
  return true
}

async function saveAgentSettings() {
  const model = selectedModel.value
  if (!model) throw new Error('请选择智能体模型')
  await settingsStore.save({
    model: { provider_id: model.provider_id, model_id: model.model_id },
    variant: selectedVariant.value === DEFAULT_VARIANT ? '' : selectedVariant.value,
    global_prompt: globalPrompt.value,
  })
  modelSnapshot.value = modelDraftSnapshot()
  promptSnapshot.value = globalPrompt.value
}

function personalizationDraftSnapshot() {
  return JSON.stringify(personalizationRules.value.map(({ id, text }) => ({ id, text })))
}

function addPersonalizationRule() {
  if (personalizationRules.value.length >= MAX_PERSONALIZATION_RULES) return
  personalizationRules.value = [...personalizationRules.value, { id: uuidv4(), text: '' }]
}

function removePersonalizationRule(id: string) {
  personalizationRules.value = personalizationRules.value.filter((rule) => rule.id !== id)
  const touched = new Set(touchedPersonalizationRuleIDs.value)
  touched.delete(id)
  touchedPersonalizationRuleIDs.value = touched
}

function touchPersonalizationRule(id: string) {
  const touched = new Set(touchedPersonalizationRuleIDs.value)
  touched.add(id)
  touchedPersonalizationRuleIDs.value = touched
}

function personalizationRuleInvalid(rule: AgentPersonalizationRule) {
  if (!touchedPersonalizationRuleIDs.value.has(rule.id)) return false
  const text = rule.text.trim()
  if (!text || new TextEncoder().encode(text).byteLength > MAX_PERSONALIZATION_RULE_BYTES) return true
  return personalizationRules.value.filter((candidate) => candidate.text.trim() === text).length > 1
}

function modelDraftSnapshot() {
  return JSON.stringify({
    model: selectedValues.value[0] || '',
    variant: selectedVariant.value,
  })
}

function requestTabChange(value: string) {
  if (!['style', 'providers', 'model', 'advanced'].includes(value) || value === activeTab.value || saving.value) return
  const nextTab = value as AgentSettingsTab
  if (currentTabDirty.value) {
    tabChangeSourceTab.value = activeTab.value
    pendingTab.value = nextTab
    tabChangeOpen.value = true
    return
  }
  activeTab.value = nextTab
}

function restoreTabDraft(tab: AgentSettingsTab) {
  if (tab === 'style') {
    displayMode.value = styleSnapshot.value
    return
  }
  if (tab === 'model' && modelSnapshot.value) {
    const snapshot = JSON.parse(modelSnapshot.value) as { model: string; variant: string }
    selectedValues.value = snapshot.model ? [snapshot.model] : []
    selectedVariant.value = snapshot.variant || DEFAULT_VARIANT
    return
  }
  if (tab !== 'advanced') return
  globalPrompt.value = promptSnapshot.value
  if (personalizationSnapshot.value) {
    personalizationRules.value = JSON.parse(personalizationSnapshot.value) as AgentPersonalizationRule[]
  }
  touchedPersonalizationRuleIDs.value = new Set()
  personalizationError.value = ''
}

function discardAndSwitchTab() {
  const nextTab = pendingTab.value
  if (!nextTab || saving.value) return
  restoreTabDraft(activeTab.value)
  activeTab.value = nextTab
  tabChangeOpen.value = false
}

async function saveAndSwitchTab() {
  const nextTab = pendingTab.value
  if (!nextTab || saving.value) return
  const saved = await saveCurrentTab(false)
  if (saved) activeTab.value = nextTab
  tabChangeOpen.value = false
}

function updateTabChangeOpen(open: boolean) {
  if (!open && !saving.value) tabChangeOpen.value = false
}

function completeTabChangeClose() {
  tabChangeSourceTab.value = null
  pendingTab.value = null
}

function tabLabel(tab: AgentSettingsTab | null) {
  return tab ? TAB_LABELS[tab] : ''
}

function updateOpen(open: boolean) {
  if (!saving.value) emit('update:open', open)
}

function handleComboboxOpen(details: { open: boolean }) {
  if (details.open) set(models.value)
}

function capabilityTags(model: AgentModelOption) {
  const tags = [
    { label: model.capabilities.input.image ? '支持图片' : '不支持图片', negative: !model.capabilities.input.image },
  ]
  if (model.capabilities.input.video) tags.push({ label: '视频输入', negative: false })
  if (model.capabilities.input.audio) tags.push({ label: '音频输入', negative: false })
  if (model.capabilities.input.pdf) tags.push({ label: 'PDF', negative: false })
  if (model.capabilities.attachment && !model.capabilities.input.image && !model.capabilities.input.pdf) {
    tags.push({ label: '附件', negative: false })
  }
  if (model.capabilities.reasoning) tags.push({ label: '推理', negative: false })
  if (model.capabilities.tools) tags.push({ label: '工具调用', negative: false })
  return tags
}

function statusLabel(status: string) {
  if (status === 'deprecated') return '已弃用'
  if (status === 'beta') return 'Beta'
  if (status === 'alpha') return 'Alpha'
  return ''
}

function variantLabel(variant: string) {
  const labels: Record<string, string> = {
    minimal: '最小',
    low: '低',
    medium: '中',
    high: '高',
    xhigh: '极高',
    max: '最大',
  }
  return labels[variant.toLocaleLowerCase()] || variant
}

function formatLimit(limit?: number) {
  if (!limit) return ''
  if (limit >= 1_000_000) return `${Math.round(limit / 100_000) / 10}M`
  if (limit >= 1_000) return `${Math.round(limit / 100) / 10}K`
  return String(limit)
}
</script>

<template>
  <Dialog.Root :open="open" :close-on-interact-outside="false" lazy-mount unmount-on-exit @update:open="updateOpen">
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner agent-settings-positioner">
        <Dialog.Content class="dialog-panel agent-settings-dialog">
          <div class="dialog-header">
            <div>
              <Dialog.Title>智能体设置</Dialog.Title>
              <Dialog.Description class="agent-settings-description">统一应用于智能体页和笔记内功能</Dialog.Description>
            </div>
            <Dialog.CloseTrigger class="dialog-close" aria-label="关闭智能体设置"
              ><CloseOutlined
            /></Dialog.CloseTrigger>
          </div>

          <div class="agent-settings-form">
            <Tabs.Root
              :model-value="activeTab"
              class="agent-settings-tabs"
              activation-mode="manual"
              @update:model-value="requestTabChange"
            >
              <Tabs.List class="agent-settings-tab-list" aria-label="智能体设置分类">
                <Tabs.Trigger class="agent-settings-tab" value="style">
                  <LayoutOutlined aria-hidden="true" />
                  <span>样式</span>
                </Tabs.Trigger>
                <Tabs.Trigger class="agent-settings-tab" value="providers">
                  <ApiOutlined aria-hidden="true" />
                  <span>提供商</span>
                </Tabs.Trigger>
                <Tabs.Trigger class="agent-settings-tab" value="model">
                  <RobotOutlined aria-hidden="true" />
                  <span>模型</span>
                </Tabs.Trigger>
                <Tabs.Trigger class="agent-settings-tab" value="advanced">
                  <ControlOutlined aria-hidden="true" />
                  <span>进阶</span>
                </Tabs.Trigger>
                <Tabs.Indicator class="agent-settings-tab-indicator" />
              </Tabs.List>

              <div class="agent-settings-scroll">
                <Tabs.Content class="agent-settings-tab-content" value="style">
                  <section class="agent-settings-section agent-personalization-section">
                    <div class="agent-settings-section-heading">
                      <div>
                        <h4>笔记内智能体布局</h4>
                        <p>选择智能体在笔记及其他内容页中的呈现方式。</p>
                      </div>
                    </div>

                    <RadioGroup.Root
                      v-model="displayMode"
                      class="agent-display-mode-group"
                      aria-label="笔记内智能体布局"
                    >
                      <RadioGroup.Item class="agent-display-mode-item" value="floating">
                        <RadioGroup.ItemHiddenInput />
                        <span class="agent-display-mode-icon"><MessageOutlined aria-hidden="true" /></span>
                        <RadioGroup.ItemText class="agent-display-mode-copy">
                          <strong>浮动按钮</strong>
                          <span>在右下角显示入口，需要时打开浮动对话窗口。</span>
                        </RadioGroup.ItemText>
                        <RadioGroup.ItemControl class="agent-display-mode-control"><span /></RadioGroup.ItemControl>
                      </RadioGroup.Item>

                      <RadioGroup.Item class="agent-display-mode-item" value="sidebar">
                        <RadioGroup.ItemHiddenInput />
                        <span class="agent-display-mode-icon"><LayoutOutlined aria-hidden="true" /></span>
                        <RadioGroup.ItemText class="agent-display-mode-copy">
                          <strong>内容右侧栏</strong>
                          <span>宽屏时持续显示独立智能体列；空间不足时自动回退为浮动按钮。</span>
                        </RadioGroup.ItemText>
                        <RadioGroup.ItemControl class="agent-display-mode-control"><span /></RadioGroup.ItemControl>
                      </RadioGroup.Item>
                    </RadioGroup.Root>
                  </section>
                </Tabs.Content>

                <Tabs.Content class="agent-settings-tab-content" value="providers">
                  <AgentProviderSettings :active="activeTab === 'providers'" @changed="refreshModels" />
                </Tabs.Content>

                <Tabs.Content class="agent-settings-tab-content" value="model">
                  <div v-if="loading" class="agent-settings-loading" aria-label="正在读取智能体设置">
                    <span class="page-loading-spinner" />
                    <span>正在读取 OpenCode 模型...</span>
                  </div>

                  <template v-else>
                    <section class="agent-settings-section">
                      <div class="agent-settings-section-heading">
                        <div>
                          <h4>模型</h4>
                          <p>仅显示 OpenCode 当前已连接的提供商与模型。</p>
                        </div>
                        <span class="agent-settings-count">{{ models.length }} 个</span>
                      </div>

                      <div v-if="unavailableModel" class="agent-settings-warning" role="status">
                        原模型 {{ unavailableModel.provider_id }}/{{ unavailableModel.model_id }}
                        当前不可用，请重新选择。
                      </div>

                      <Combobox.Root
                        v-model="selectedValues"
                        :collection="collection"
                        :disabled="models.length === 0"
                        :positioning="{ placement: 'bottom-start', sameWidth: true }"
                        input-behavior="autohighlight"
                        open-on-click
                        selection-behavior="clear"
                        @input-value-change="filter($event.inputValue)"
                        @open-change="handleComboboxOpen"
                      >
                        <Combobox.Label class="agent-settings-field-label">选择模型</Combobox.Label>
                        <Combobox.Control class="agent-model-control">
                          <Combobox.Input
                            class="agent-model-input"
                            placeholder="搜索模型、提供商或 ID"
                            aria-label="选择智能体模型"
                          />
                          <Combobox.Trigger class="agent-model-trigger" aria-label="展开模型列表"
                            ><DownOutlined
                          /></Combobox.Trigger>
                        </Combobox.Control>
                        <Teleport to="body">
                          <Combobox.Positioner class="agent-model-positioner">
                            <Combobox.Content class="agent-model-content">
                              <Combobox.Empty class="agent-model-empty">没有匹配的模型</Combobox.Empty>
                              <Combobox.Item
                                v-for="model in collection.items"
                                :key="modelKey(model)"
                                :item="model"
                                class="agent-model-item"
                              >
                                <div class="agent-model-item-main">
                                  <div class="agent-model-name-line">
                                    <Combobox.ItemText class="agent-model-name">{{ model.name }}</Combobox.ItemText>
                                    <span v-if="statusLabel(model.status)" class="agent-model-status">{{
                                      statusLabel(model.status)
                                    }}</span>
                                  </div>
                                  <div class="agent-model-id">{{ model.provider_name }} · {{ model.model_id }}</div>
                                  <div class="agent-model-tags">
                                    <span
                                      v-for="tag in capabilityTags(model)"
                                      :key="tag.label"
                                      class="agent-model-tag"
                                      :class="{ negative: tag.negative }"
                                      >{{ tag.label }}</span
                                    >
                                    <span v-if="model.context_limit" class="agent-model-tag subtle"
                                      >上下文 {{ formatLimit(model.context_limit) }}</span
                                    >
                                  </div>
                                </div>
                                <Combobox.ItemIndicator class="agent-model-check"
                                  ><CheckOutlined
                                /></Combobox.ItemIndicator>
                              </Combobox.Item>
                            </Combobox.Content>
                          </Combobox.Positioner>
                        </Teleport>
                      </Combobox.Root>

                      <div v-if="selectedModel" class="agent-model-selected" aria-live="polite">
                        <div class="agent-model-selected-top">
                          <strong>{{ selectedModel.name }}</strong>
                          <span>{{ selectedModel.provider_name }}</span>
                        </div>
                        <div class="agent-model-tags">
                          <span
                            v-for="tag in capabilityTags(selectedModel)"
                            :key="tag.label"
                            class="agent-model-tag"
                            :class="{ negative: tag.negative }"
                            >{{ tag.label }}</span
                          >
                        </div>
                      </div>

                      <div v-if="selectedModel" class="agent-variant-field">
                        <div class="agent-variant-heading">
                          <span class="agent-settings-field-label">推理强度</span>
                          <span>由后端在每次请求中覆盖</span>
                        </div>
                        <div v-if="unavailableVariant" class="agent-settings-warning" role="status">
                          原推理强度 {{ unavailableVariant }} 已不受当前模型支持，已切换为模型默认。
                        </div>
                        <SegmentGroup.Root
                          v-if="selectedModel.variants.length"
                          v-model="selectedVariant"
                          class="agent-variant-group"
                          aria-label="推理强度"
                        >
                          <SegmentGroup.Indicator class="agent-variant-indicator" />
                          <SegmentGroup.Item
                            v-for="option in variantOptions"
                            :key="option.value"
                            :value="option.value"
                            class="agent-variant-item"
                            :title="
                              option.value === DEFAULT_VARIANT ? '不指定 variant，使用模型默认行为' : option.value
                            "
                          >
                            <SegmentGroup.ItemText>{{ option.label }}</SegmentGroup.ItemText>
                            <SegmentGroup.ItemHiddenInput />
                          </SegmentGroup.Item>
                        </SegmentGroup.Root>
                        <p v-else class="agent-variant-unavailable">该模型未提供可调档位，将使用模型默认行为。</p>
                      </div>
                    </section>

                    <div v-if="error" class="agent-settings-error" role="alert">{{ error }}</div>
                  </template>
                </Tabs.Content>

                <Tabs.Content class="agent-settings-tab-content" value="advanced">
                  <div v-if="loading" class="agent-settings-loading" aria-label="正在读取进阶设置">
                    <span class="page-loading-spinner" />
                    <span>正在读取进阶设置...</span>
                  </div>

                  <template v-else>
                    <section class="agent-settings-section">
                      <Field.Root :invalid="promptTooLarge">
                        <Field.Label class="agent-settings-field-label">全局提示词</Field.Label>
                        <XFullscreenTextarea
                          v-model="globalPrompt"
                          class="agent-settings-prompt"
                          title="全屏编辑全局提示词"
                          :aria-describedby="promptTooLarge ? 'agent-prompt-error' : 'agent-prompt-help'"
                          placeholder="例如：默认使用中文回答；修改笔记前先说明计划……"
                        />
                        <div class="agent-settings-prompt-meta">
                          <Field.HelperText id="agent-prompt-help"
                            >每次请求都由后端注入；与 Marvo 基础规则冲突时，基础规则优先。</Field.HelperText
                          >
                          <span :class="{ invalid: promptTooLarge }"
                            >{{ promptBytes.toLocaleString() }} / {{ MAX_PROMPT_BYTES.toLocaleString() }} 字节</span
                          >
                        </div>
                        <Field.ErrorText v-if="promptTooLarge" id="agent-prompt-error"
                          >全局提示词不能超过 64 KiB。</Field.ErrorText
                        >
                      </Field.Root>
                    </section>

                    <section class="agent-settings-section agent-personalization-rules-section">
                      <div class="agent-settings-section-heading">
                        <div>
                          <h4>个性化规则</h4>
                          <p>你与智能体共同维护的长期默认偏好；当前请求中的明确要求仍然优先。</p>
                        </div>
                        <span class="agent-settings-count">{{ personalizationRules.length }} 条</span>
                      </div>

                      <div v-if="personalizationLoading" class="agent-personalization-loading" role="status">
                        <span class="page-loading-spinner" />
                        <span>正在读取规则...</span>
                      </div>

                      <template v-else-if="personalizationReady">
                        <div v-if="personalizationRules.length" class="agent-personalization-rules">
                          <Field.Root
                            v-for="(rule, index) in personalizationRules"
                            :key="rule.id"
                            class="agent-personalization-rule"
                            :invalid="personalizationRuleInvalid(rule)"
                          >
                            <Field.Input
                              v-model="rule.text"
                              class="agent-personalization-input"
                              :aria-label="`个性化规则 ${index + 1}`"
                              placeholder="例如：面向用户时统一使用“智能体”这一称呼"
                              @blur="touchPersonalizationRule(rule.id)"
                            />
                            <button
                              type="button"
                              class="agent-personalization-delete"
                              :aria-label="`删除个性化规则 ${index + 1}`"
                              @click="removePersonalizationRule(rule.id)"
                            >
                              <DeleteOutlined aria-hidden="true" />
                              <span>删除</span>
                            </button>
                            <Field.ErrorText>规则不能为空、重复或超过 4 KiB。</Field.ErrorText>
                          </Field.Root>
                        </div>
                        <div v-else class="agent-personalization-empty">
                          尚无个性化规则，智能体也可以在收到长期偏好反馈后添加。
                        </div>

                        <button
                          type="button"
                          class="admin-btn agent-personalization-add"
                          :disabled="personalizationRules.length >= MAX_PERSONALIZATION_RULES"
                          @click="addPersonalizationRule"
                        >
                          <PlusOutlined aria-hidden="true" />
                          <span>新增规则</span>
                        </button>
                      </template>

                      <div
                        v-if="personalizationError"
                        class="agent-settings-error agent-personalization-error"
                        role="alert"
                      >
                        {{ personalizationError }}
                      </div>
                    </section>

                    <div v-if="error" class="agent-settings-error" role="alert">{{ error }}</div>
                  </template>
                </Tabs.Content>
              </div>
            </Tabs.Root>

            <footer v-if="activeTab !== 'providers'" class="agent-settings-footer">
              <button
                type="button"
                class="admin-btn agent-settings-action"
                :disabled="saving"
                @click="updateOpen(false)"
              >
                <CloseOutlined aria-hidden="true" />
                <span>取消</span>
              </button>
              <button
                type="button"
                class="admin-btn admin-btn-primary agent-settings-action"
                :disabled="!canSave"
                @click="saveSettings"
              >
                <SaveOutlined aria-hidden="true" />
                <span>{{ saving ? '保存中...' : '保存设置' }}</span>
              </button>
            </footer>
          </div>

          <Dialog.Root
            :open="tabChangeOpen"
            :close-on-interact-outside="false"
            lazy-mount
            unmount-on-exit
            @exit-complete="completeTabChangeClose"
            @update:open="updateTabChangeOpen"
          >
            <Teleport to="body">
              <Dialog.Backdrop class="dialog-backdrop agent-tab-change-backdrop" />
              <Dialog.Positioner class="dialog-positioner agent-tab-change-positioner">
                <Dialog.Content class="dialog-panel agent-tab-change-dialog">
                  <div class="dialog-header">
                    <div>
                      <Dialog.Title>有未保存的设置</Dialog.Title>
                      <Dialog.Description>
                        “{{ tabLabel(tabChangeSourceTab) }}”中的修改尚未保存。保存或放弃后，再前往“{{
                          tabLabel(pendingTab)
                        }}”。
                      </Dialog.Description>
                    </div>
                  </div>
                  <div class="agent-tab-change-actions">
                    <Dialog.CloseTrigger as-child>
                      <button type="button" class="admin-btn" :disabled="saving">
                        <CloseOutlined aria-hidden="true" />
                        <span>继续编辑</span>
                      </button>
                    </Dialog.CloseTrigger>
                    <button
                      type="button"
                      class="admin-btn admin-btn-danger"
                      :disabled="saving"
                      @click="discardAndSwitchTab"
                    >
                      <DeleteOutlined aria-hidden="true" />
                      <span>放弃并切换</span>
                    </button>
                    <button
                      type="button"
                      class="admin-btn admin-btn-primary"
                      :disabled="!canSave"
                      @click="saveAndSwitchTab"
                    >
                      <SaveOutlined aria-hidden="true" />
                      <span>{{ saving ? '保存中...' : '保存并切换' }}</span>
                    </button>
                  </div>
                </Dialog.Content>
              </Dialog.Positioner>
            </Teleport>
          </Dialog.Root>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>
</template>

<style lang="scss">
.agent-settings-dialog {
  max-width: 680px;
}
.agent-settings-dialog .dialog-header {
  flex-shrink: 0;
  align-items: flex-start;
}
.agent-settings-description {
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.agent-settings-form {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
}
.agent-settings-tabs {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
}
.agent-settings-tab-list {
  position: relative;
  display: flex;
  flex-shrink: 0;
  gap: 4px;
  padding: 0 24px;
  border-bottom: 1px solid var(--border-primary);
}
.agent-settings-tab {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-width: 112px;
  height: 44px;
  padding: 0 14px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-13);
  transition: color 0.15s;
}
.agent-settings-tab:hover,
.agent-settings-tab[data-selected] {
  color: var(--text-accent);
}
.agent-settings-tab:focus-visible {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--marvo-accent-color) 28%, transparent);
}
.agent-settings-tab-indicator {
  position: absolute;
  bottom: -1px;
  height: 2px;
  border-radius: 2px 2px 0 0;
  background: var(--marvo-accent-color);
  transition:
    left 0.18s ease,
    width 0.18s ease;
}
.agent-settings-scroll {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}
.agent-settings-tab-content {
  min-height: 100%;
  outline: 0;
}
.agent-settings-tab-content > .agent-settings-section:first-child {
  padding-top: 22px;
}
.agent-settings-loading {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--text-muted);
  font-size: var(--marvo-type-13);
}
.agent-settings-loading .page-loading-spinner {
  position: static;
}
.agent-settings-section {
  padding: 0 24px 22px;
}
.agent-settings-section + .agent-settings-section {
  padding-top: 20px;
  border-top: 1px solid var(--border-primary);
}
.agent-settings-section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.agent-settings-section-heading h4 {
  margin: 0 0 4px;
  color: var(--text-primary);
  font-size: var(--marvo-type-14);
}
.agent-settings-section-heading p {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.agent-settings-count {
  flex-shrink: 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.agent-display-mode-group {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.agent-display-mode-item {
  position: relative;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
  min-height: 116px;
  box-sizing: border-box;
  padding: 16px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
  color: var(--text-primary);
  cursor: pointer;
  outline: 0;
  transition:
    border-color 0.15s,
    background 0.15s,
    box-shadow 0.15s;
}
.agent-display-mode-item:hover {
  border-color: var(--border-light);
  background: var(--bg-hover);
}
.agent-display-mode-item[data-state='checked'] {
  border-color: var(--text-accent);
  background: color-mix(in srgb, var(--marvo-accent-color) 6%, var(--bg-primary));
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--marvo-accent-color) 10%, transparent);
}
.agent-display-mode-item:focus-visible {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 22%, transparent);
}
.agent-display-mode-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 9px;
  background: var(--bg-secondary);
  color: var(--text-accent);
  font-size: var(--marvo-type-17);
}
.agent-display-mode-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 5px;
}
.agent-display-mode-copy strong {
  font-size: var(--marvo-type-13);
  line-height: 1.4;
}
.agent-display-mode-copy > span {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.55;
}
.agent-display-mode-control {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  box-sizing: border-box;
  border: 1px solid var(--border-light);
  border-radius: 50%;
  background: var(--bg-card);
}
.agent-display-mode-control[data-state='checked'] {
  border: 5px solid var(--marvo-accent-color);
}
.agent-personalization-loading {
  display: flex;
  min-height: 92px;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.agent-personalization-loading .page-loading-spinner {
  position: static;
}
.agent-personalization-rules {
  display: flex;
  flex-direction: column;
  gap: 9px;
}
.agent-personalization-rule {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 7px;
}
.agent-personalization-input {
  min-width: 0;
  height: 38px;
  box-sizing: border-box;
  padding: 0 11px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-12);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.agent-personalization-input:focus {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}
.agent-personalization-rule[data-invalid] .agent-personalization-input {
  border-color: var(--text-danger);
}
.agent-personalization-delete,
.agent-personalization-add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}
.agent-personalization-delete {
  height: 38px;
  padding: 0 10px;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-muted);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-11);
}
.agent-personalization-delete:hover {
  border-color: color-mix(in srgb, var(--text-danger) 42%, var(--border-primary));
  color: var(--text-danger);
}
.agent-personalization-delete:focus-visible {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 18%, transparent);
}
.agent-personalization-rule [data-part='error-text'] {
  grid-column: 1 / -1;
  margin: -2px 0 1px;
}
.agent-personalization-empty {
  padding: 18px;
  border: 1px dashed var(--border-primary);
  border-radius: 9px;
  background: var(--bg-secondary);
  color: var(--text-muted);
  text-align: center;
  font-size: var(--marvo-type-12);
  line-height: 1.6;
}
.agent-personalization-add {
  margin-top: 11px;
}
.agent-personalization-error {
  margin: 12px 0 0;
}
.agent-settings-field-label {
  display: block;
  margin-bottom: 7px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  font-weight: 600;
}
.agent-settings-warning,
.agent-settings-error {
  margin-bottom: 12px;
  padding: 9px 11px;
  border-radius: 7px;
  font-size: var(--marvo-type-12);
  line-height: 1.5;
}
.agent-settings-warning {
  background: color-mix(in srgb, #d97706 12%, transparent);
  color: #b45309;
}
:root[data-color-scheme='dark'] .agent-settings-warning {
  color: #fbbf24;
}
.agent-settings-error {
  margin: 0 24px 16px;
  background: color-mix(in srgb, var(--text-danger) 10%, transparent);
  color: var(--text-danger);
}
.agent-model-control {
  display: flex;
  align-items: center;
  width: 100%;
  height: 42px;
  overflow: hidden;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  background: var(--bg-primary);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.agent-model-control:focus-within {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}
.agent-model-control[data-disabled] {
  opacity: 0.55;
}
.agent-model-input {
  flex: 1;
  min-width: 0;
  height: 100%;
  padding: 0 12px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
}
.agent-model-input::placeholder {
  color: var(--text-muted);
}
.agent-model-trigger {
  width: 40px;
  height: 100%;
  border: 0;
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
}
.agent-model-positioner {
  z-index: 1100 !important;
}
.agent-model-content {
  width: var(--reference-width);
  max-width: calc(100vw - 24px);
  max-height: min(390px, 55vh);
  overflow-y: auto;
  padding: 5px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
  outline: none;
}
.agent-model-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 11px;
  border-radius: 7px;
  cursor: pointer;
  color: var(--text-primary);
  outline: none;
}
.agent-model-item[data-highlighted] {
  background: var(--bg-hover);
}
.agent-model-item[data-state='checked'] {
  background: color-mix(in srgb, var(--marvo-accent-color) 9%, transparent);
}
.agent-model-item-main {
  min-width: 0;
  flex: 1;
}
.agent-model-name-line,
.agent-model-selected-top {
  display: flex;
  align-items: center;
  gap: 7px;
}
.agent-model-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--marvo-type-13);
  font-weight: 600;
}
.agent-model-status {
  padding: 1px 5px;
  border-radius: 4px;
  background: color-mix(in srgb, #d97706 13%, transparent);
  color: #b45309;
  font-size: var(--marvo-type-10);
}
.agent-model-id {
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.agent-model-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-top: 7px;
}
.agent-model-tag {
  padding: 2px 6px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-10);
  line-height: 1.4;
}
.agent-model-tag.negative,
.agent-model-tag.subtle {
  background: var(--bg-secondary);
  color: var(--text-muted);
}
.agent-model-check {
  flex-shrink: 0;
  color: var(--text-accent);
}
.agent-model-empty {
  padding: 24px;
  text-align: center;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.agent-model-selected {
  margin-top: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  background: var(--bg-secondary);
}
.agent-model-selected-top strong {
  color: var(--text-primary);
  font-size: var(--marvo-type-12);
}
.agent-model-selected-top span {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.agent-variant-field {
  margin-top: 16px;
}
.agent-variant-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
}
.agent-variant-heading .agent-settings-field-label {
  margin: 0;
}
.agent-variant-heading > span:last-child {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.agent-variant-group {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
  width: fit-content;
  max-width: 100%;
  padding: 3px;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  background: var(--bg-secondary);
}
.agent-variant-indicator {
  position: absolute;
  border-radius: 6px;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}
.agent-variant-item {
  position: relative;
  z-index: 1;
  min-width: 48px;
  padding: 6px 10px;
  border-radius: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  text-align: center;
  font-size: var(--marvo-type-12);
  outline: none;
  transition: color 0.15s;
}
.agent-variant-item[data-state='checked'] {
  color: var(--text-accent);
  font-weight: 600;
}
.agent-variant-item:focus-visible {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--marvo-accent-color) 30%, transparent);
}
.agent-variant-unavailable {
  margin: 0;
  padding: 9px 11px;
  border-radius: 7px;
  background: var(--bg-secondary);
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.agent-settings-prompt {
  box-sizing: border-box;
  width: 100%;
  min-height: 116px;
  max-height: 240px;
  resize: vertical;
  padding: 10px 12px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
  line-height: 1.6;
}
.agent-settings-prompt:focus {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}
.agent-settings-prompt-meta {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 6px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.agent-settings-prompt-meta [data-part='helper-text'] {
  margin: 0;
}
.agent-settings-prompt-meta .invalid,
.agent-settings-dialog [data-part='error-text'] {
  color: var(--text-danger);
}
.agent-settings-dialog [data-part='error-text'] {
  margin-top: 5px;
  font-size: var(--marvo-type-11);
}
.agent-settings-footer {
  display: flex;
  flex-shrink: 0;
  justify-content: flex-end;
  gap: 8px;
  padding: 14px 24px 18px;
  border-top: 1px solid var(--border-primary);
  background: var(--bg-card);
}
.agent-settings-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
}
.agent-tab-change-backdrop,
.agent-tab-change-positioner {
  --dialog-z-index: 1500;
}
.agent-tab-change-dialog {
  max-width: 460px;
}
.agent-tab-change-dialog .dialog-header {
  padding-bottom: 12px;
}
.agent-tab-change-dialog [data-part='description'] {
  margin: 7px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  line-height: 1.6;
}
.agent-tab-change-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 4px 20px 20px;
}
.agent-tab-change-actions .admin-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

@media (max-width: 600px) {
  .agent-settings-positioner {
    align-items: flex-end;
    padding: 0;
  }
  .agent-settings-dialog {
    width: 100%;
    max-width: none;
    max-height: min(92dvh, 760px);
    border-radius: 14px 14px 0 0;
  }
  .agent-settings-section {
    padding-inline: 16px;
  }
  .agent-settings-dialog .dialog-header {
    padding-inline: 16px;
  }
  .agent-settings-tab-list {
    padding-inline: 16px;
  }
  .agent-settings-tab {
    min-width: 0;
    flex: 1;
  }
  .agent-display-mode-group {
    grid-template-columns: 1fr;
  }
  .agent-settings-footer {
    padding-inline: 16px;
    padding-bottom: max(18px, env(safe-area-inset-bottom));
  }
  .agent-tab-change-positioner {
    align-items: flex-end;
    padding: 0;
  }
  .agent-tab-change-dialog {
    width: 100%;
    max-width: none;
    border-radius: 14px 14px 0 0;
  }
  .agent-tab-change-actions {
    padding-bottom: max(20px, env(safe-area-inset-bottom));
  }
  .agent-settings-prompt-meta {
    align-items: flex-end;
    flex-direction: column;
    gap: 3px;
  }
}
</style>
