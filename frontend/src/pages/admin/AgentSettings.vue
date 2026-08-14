<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Combobox, useListCollection } from '@ark-ui/vue/combobox'
import { Field } from '@ark-ui/vue/field'
import { SegmentGroup } from '@ark-ui/vue/segment-group'
import {
  CheckOutlined,
  CloseOutlined,
  DeleteOutlined,
  DownOutlined,
  GlobalOutlined,
  PlusOutlined,
  SaveOutlined,
  UndoOutlined,
} from '@ant-design/icons-vue'
import { v4 as uuidv4 } from 'uuid'
import { useAgentPersonalizationStore } from '../../stores/agentPersonalization'
import { useAgentSettingsStore } from '../../stores/agentSettings'
import type { AgentModelOption, AgentModelSelection, AgentPersonalizationRule, AgentSettingsUpdate } from '../../sdk'
import AgentProviderSettings from '../../components/AgentProviderSettings.vue'
import { XFullscreenTextarea } from '../../components/x'

const MAX_PROMPT_BYTES = 64 * 1024
const MAX_EXA_API_KEY_BYTES = 4 * 1024
const MAX_PERSONALIZATION_RULE_BYTES = 4 * 1024
const MAX_PERSONALIZATION_RULES = 256
const DEFAULT_VARIANT = '__model_default__'
const settingsStore = useAgentSettingsStore()
const personalizationStore = useAgentPersonalizationStore()
const loading = ref(false)
const saving = ref(false)
const settingsReady = ref(false)
const modelSnapshot = ref('')
const promptSnapshot = ref('')
const error = ref('')
const models = ref<AgentModelOption[]>([])
const selectedValues = ref<string[]>([])
const selectedVariant = ref(DEFAULT_VARIANT)
const variantScroller = ref<HTMLElement>()
const globalPrompt = ref('')
const exaConfigured = ref(false)
const exaAPIKey = ref('')
const exaClearRequested = ref(false)
const unavailableModel = ref<AgentModelSelection | null>(null)
const unavailableVariant = ref('')
const personalizationRules = ref<AgentPersonalizationRule[]>([])
const personalizationRevision = ref('')
const personalizationSnapshot = ref('')
const personalizationReady = ref(false)
const personalizationLoading = ref(false)
const personalizationError = ref('')
const touchedPersonalizationRuleIDs = ref<Set<string>>(new Set())
const pageHeading = ref<HTMLElement | null>(null)
const pageHeadingVisible = ref(true)
let loadSequence = 0
let personalizationLoadSequence = 0
let pageHeadingObserver: IntersectionObserver | null = null

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
const exaAPIKeyBytes = computed(() => new TextEncoder().encode(exaAPIKey.value.trim()).byteLength)
const exaAPIKeyTooLarge = computed(() => exaAPIKeyBytes.value > MAX_EXA_API_KEY_BYTES)
const modelDirty = computed(() => settingsReady.value && modelSnapshot.value !== modelDraftSnapshot())
const promptDirty = computed(() => settingsReady.value && promptSnapshot.value !== globalPrompt.value)
const exaDirty = computed(() => settingsReady.value && (exaAPIKey.value.trim().length > 0 || exaClearRequested.value))
const personalizationDirty = computed(
  () => personalizationReady.value && personalizationSnapshot.value !== personalizationDraftSnapshot(),
)
const settingsDirty = computed(
  () => modelDirty.value || promptDirty.value || exaDirty.value || personalizationDirty.value,
)
const exaStatus = computed(() => {
  if (!settingsReady.value) return loading.value ? '读取中' : '不可用'
  if (exaClearRequested.value) return '等待移除'
  if (exaAPIKey.value.trim()) return exaConfigured.value ? '等待替换' : '等待保存'
  return exaConfigured.value ? '已配置' : '匿名额度'
})
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
const canSave = computed(() => !saving.value && settingsDirty.value)
const showFloatingActions = computed(() => settingsDirty.value && !pageHeadingVisible.value)

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

watch([selectedVariant, () => variantOptions.value.length, variantScroller], () => revealSelectedVariant(), {
  flush: 'post',
})

onMounted(() => {
  touchedPersonalizationRuleIDs.value = new Set()
  if (typeof IntersectionObserver !== 'undefined' && pageHeading.value) {
    pageHeadingObserver = new IntersectionObserver(
      ([entry]) => {
        pageHeadingVisible.value = entry?.isIntersecting ?? true
      },
      { threshold: 0.5 },
    )
    pageHeadingObserver.observe(pageHeading.value)
  }
  void loadSettings()
  void loadPersonalization()
})

onBeforeUnmount(() => pageHeadingObserver?.disconnect())

function modelKey(model: Pick<AgentModelOption, 'provider_id' | 'model_id'>) {
  return JSON.stringify([model.provider_id, model.model_id])
}

function revealSelectedVariant() {
  void nextTick(() => {
    const scroller = variantScroller.value
    const selected = scroller?.querySelector<HTMLElement>('.agent-variant-item[data-state="checked"]')
    if (!scroller || !selected || scroller.scrollWidth <= scroller.clientWidth) return

    const scrollerBounds = scroller.getBoundingClientRect()
    const selectedBounds = selected.getBoundingClientRect()
    if (selectedBounds.left >= scrollerBounds.left && selectedBounds.right <= scrollerBounds.right) return

    const selectedCenter = selectedBounds.left - scrollerBounds.left + scroller.scrollLeft + selectedBounds.width / 2
    scroller.scrollLeft = Math.max(0, selectedCenter - scroller.clientWidth / 2)
  })
}

async function loadPersonalization() {
  const sequence = ++personalizationLoadSequence
  personalizationLoading.value = true
  personalizationReady.value = false
  personalizationError.value = ''
  try {
    const snapshot = await personalizationStore.load(true)
    if (sequence !== personalizationLoadSequence) return
    personalizationRules.value = snapshot.rules.map((rule) => ({ ...rule }))
    personalizationRevision.value = snapshot.revision
    touchedPersonalizationRuleIDs.value = new Set()
    personalizationReady.value = true
    personalizationSnapshot.value = personalizationDraftSnapshot()
  } catch (cause) {
    if (sequence !== personalizationLoadSequence) return
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
  exaAPIKey.value = ''
  exaClearRequested.value = false
  try {
    const settings = await settingsStore.load(true)
    if (sequence !== loadSequence) return
    models.value = Array.isArray(settings.models)
      ? settings.models.map((model) => ({ ...model, variants: Array.isArray(model.variants) ? model.variants : [] }))
      : []
    set(models.value)
    globalPrompt.value = settings.global_prompt || ''
    exaConfigured.value = Boolean(settings.exa_configured)
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
    if (sequence !== loadSequence) return
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
  if (!canSave.value) return false
  if (!validateSettings()) return false
  let savePhase: 'personalization' | 'settings' = 'settings'
  saving.value = true
  error.value = ''
  personalizationError.value = ''
  try {
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
    if (modelDirty.value || promptDirty.value || exaDirty.value) {
      savePhase = 'settings'
      await saveAgentSettings()
    }
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

function validateSettings() {
  if (
    (modelDirty.value || promptDirty.value || exaDirty.value) &&
    (!settingsReady.value || !selectedModel.value || loading.value)
  ) {
    error.value = '请选择智能体模型'
    return false
  }
  if (promptTooLarge.value) return false
  if (exaAPIKeyTooLarge.value) return false
  if (personalizationDirty.value && personalizationInvalid.value) {
    touchedPersonalizationRuleIDs.value = new Set(personalizationRules.value.map((rule) => rule.id))
    return false
  }
  return true
}

async function saveAgentSettings() {
  const model = selectedModel.value
  if (!model) throw new Error('请选择智能体模型')
  const update = {
    model: { provider_id: model.provider_id, model_id: model.model_id },
    variant: selectedVariant.value === DEFAULT_VARIANT ? '' : selectedVariant.value,
    global_prompt: globalPrompt.value,
  } as AgentSettingsUpdate
  const key = exaAPIKey.value.trim()
  if (key) update.exa_api_key = key
  else if (exaClearRequested.value) update.clear_exa_api_key = true
  const settings = await settingsStore.save(update)
  exaConfigured.value = Boolean(settings.exa_configured)
  exaAPIKey.value = ''
  exaClearRequested.value = false
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

function restoreSettingsDraft() {
  if (modelSnapshot.value) {
    const snapshot = JSON.parse(modelSnapshot.value) as { model: string; variant: string }
    selectedValues.value = snapshot.model ? [snapshot.model] : []
    selectedVariant.value = snapshot.variant || DEFAULT_VARIANT
  }
  globalPrompt.value = promptSnapshot.value
  exaAPIKey.value = ''
  exaClearRequested.value = false
  if (personalizationSnapshot.value) {
    personalizationRules.value = JSON.parse(personalizationSnapshot.value) as AgentPersonalizationRule[]
  }
  touchedPersonalizationRuleIDs.value = new Set()
  personalizationError.value = ''
}

function handleExaAPIKeyInput() {
  exaClearRequested.value = false
}

function toggleExaRemoval() {
  exaAPIKey.value = ''
  exaClearRequested.value = !exaClearRequested.value
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
    none: '关闭',
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
  <section class="agent-settings-page">
    <header class="agent-settings-page-heading">
      <div ref="pageHeading" class="agent-settings-page-heading-copy">
        <h1>智能体设置</h1>
        <p>管理当前用户空间的提供商、模型与长期偏好。</p>
      </div>
      <div v-if="!showFloatingActions" class="agent-settings-page-actions">
        <span v-if="settingsDirty" class="agent-settings-unsaved-state" role="status">
          <span aria-hidden="true" />有未保存修改
        </span>
        <button
          type="button"
          class="admin-btn agent-settings-action agent-settings-action-discard"
          :disabled="saving || !settingsDirty"
          @click="restoreSettingsDraft"
        >
          <CloseOutlined aria-hidden="true" />
          <span>放弃修改</span>
        </button>
        <button
          type="button"
          class="admin-btn agent-settings-action agent-settings-action-save"
          :disabled="!canSave"
          @click="saveSettings"
        >
          <SaveOutlined aria-hidden="true" />
          <span>{{ saving ? '保存中...' : '保存设置' }}</span>
        </button>
      </div>
    </header>
    <div class="agent-settings-body">
      <div class="agent-settings-form agent-settings-sections">
        <div class="agent-runtime-grid">
          <AgentProviderSettings :active="true" @changed="refreshModels" />

          <section class="agent-settings-block" aria-labelledby="agent-model-heading">
            <div v-if="loading" class="agent-settings-loading" aria-label="正在读取智能体设置">
              <span class="page-loading-spinner" />
              <span>正在读取 OpenCode 模型...</span>
            </div>

            <template v-else>
              <section class="agent-settings-section agent-model-section">
                <div class="agent-settings-section-heading">
                  <div>
                    <h2 id="agent-model-heading">模型</h2>
                    <p>仅显示 OpenCode 当前已连接的提供商与模型。</p>
                  </div>
                  <span class="agent-settings-count">{{ models.length }} 个</span>
                </div>

                <div v-if="unavailableModel" class="agent-settings-warning" role="status">
                  原模型 {{ unavailableModel.provider_id }}/{{ unavailableModel.model_id }}
                  当前不可用，请重新选择。
                </div>

                <div class="agent-model-layout">
                  <div class="agent-model-picker-column">
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
                    <p class="agent-model-picker-hint">模型来自已连接的提供商；切换提供商后列表会自动更新。</p>
                  </div>

                  <div class="agent-model-configuration">
                    <div class="agent-model-configuration-heading">当前配置</div>
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
                    <p v-else class="agent-model-configuration-empty">
                      连接提供商并选择一个模型后，可在这里配置推理强度。
                    </p>

                    <div v-if="selectedModel" class="agent-variant-field">
                      <div class="agent-variant-heading">
                        <span class="agent-settings-field-label">推理强度</span>
                        <span>由后端在每次请求中覆盖</span>
                      </div>
                      <div v-if="unavailableVariant" class="agent-settings-warning" role="status">
                        原推理强度 {{ unavailableVariant }} 已不受当前模型支持，已切换为模型默认。
                      </div>
                      <div v-if="selectedModel.variants.length" ref="variantScroller" class="agent-variant-scroll">
                        <SegmentGroup.Root v-model="selectedVariant" class="agent-variant-group" aria-label="推理强度">
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
                      </div>
                      <p v-else class="agent-variant-unavailable">该模型未提供可调档位，将使用模型默认行为。</p>
                    </div>
                  </div>
                </div>
              </section>
            </template>
          </section>
        </div>

        <section class="agent-settings-section agent-exa-section" aria-labelledby="agent-exa-heading">
          <div class="agent-settings-section-heading">
            <div>
              <h2 id="agent-exa-heading">联网搜索</h2>
              <p>为 OpenCode 内置的 Exa 搜索配置个人额度，避免匿名服务的共享限流。</p>
            </div>
            <span class="agent-exa-status" :data-state="exaStatus">{{ exaStatus }}</span>
          </div>

          <div class="agent-exa-layout">
            <div class="agent-exa-summary">
              <span class="agent-exa-icon"><GlobalOutlined aria-hidden="true" /></span>
              <div>
                <strong>Exa API</strong>
                <p>未配置时仍会使用 OpenCode 官方匿名额度；配置后，当前用户空间的联网搜索将使用你的 Exa Key。</p>
              </div>
            </div>

            <Field.Root class="agent-exa-field" :invalid="exaAPIKeyTooLarge">
              <Field.Label class="agent-settings-field-label">Exa API Key</Field.Label>
              <div class="agent-exa-control">
                <Field.Input
                  v-model="exaAPIKey"
                  type="password"
                  class="agent-exa-input"
                  autocomplete="new-password"
                  :disabled="!settingsReady || loading || exaClearRequested"
                  :placeholder="exaConfigured ? '已配置；输入新密钥可替换' : '粘贴 Exa API Key'"
                  aria-label="Exa API Key"
                  aria-describedby="agent-exa-help"
                  @input="handleExaAPIKeyInput"
                />
                <button
                  v-if="exaConfigured"
                  type="button"
                  class="admin-btn agent-exa-remove"
                  :disabled="!settingsReady || loading"
                  @click="toggleExaRemoval"
                >
                  <UndoOutlined v-if="exaClearRequested" aria-hidden="true" />
                  <DeleteOutlined v-else aria-hidden="true" />
                  <span>{{ exaClearRequested ? '保留密钥' : '移除密钥' }}</span>
                </button>
              </div>
              <Field.HelperText id="agent-exa-help" class="agent-exa-help">
                密钥只发送到 Marvo
                后端并加密保存在当前用户空间，浏览器不会读取已保存的原值。保存后将在下一次智能体请求前更新运行环境。
              </Field.HelperText>
              <Field.ErrorText v-if="exaAPIKeyTooLarge">Exa API Key 不能超过 4 KiB。</Field.ErrorText>
            </Field.Root>
          </div>
        </section>

        <section class="agent-settings-block" aria-label="进阶设置">
          <div v-if="loading" class="agent-settings-loading" aria-label="正在读取进阶设置">
            <span class="page-loading-spinner" />
            <span>正在读取进阶设置...</span>
          </div>

          <template v-else>
            <div class="agent-advanced-grid">
              <section class="agent-settings-section">
                <div class="agent-settings-section-heading">
                  <div>
                    <h2>全局提示词</h2>
                    <p>每次请求都由后端注入；与 Marvo 基础规则冲突时，基础规则优先。</p>
                  </div>
                </div>
                <Field.Root :invalid="promptTooLarge">
                  <Field.Label class="agent-settings-visually-hidden">全局提示词</Field.Label>
                  <XFullscreenTextarea
                    v-model="globalPrompt"
                    class="agent-settings-prompt"
                    title="全屏编辑全局提示词"
                    :aria-describedby="promptTooLarge ? 'agent-prompt-error' : 'agent-prompt-help'"
                    placeholder="例如：默认使用中文回答；修改笔记前先说明计划……"
                  />
                  <div class="agent-settings-prompt-meta">
                    <Field.HelperText id="agent-prompt-help">应用于当前用户空间内的所有智能体请求。</Field.HelperText>
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
                    <h2>个性化规则</h2>
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

                <div v-if="personalizationError" class="agent-settings-error agent-personalization-error" role="alert">
                  {{ personalizationError }}
                </div>
              </section>
            </div>
          </template>
        </section>

        <div v-if="error" class="agent-settings-error" role="alert">{{ error }}</div>
      </div>
    </div>
  </section>

  <Teleport to="body">
    <Transition name="agent-settings-dirty-bar">
      <div v-if="showFloatingActions" class="agent-settings-dirty-bar" aria-label="未保存的智能体设置">
        <span class="agent-settings-unsaved-state" role="status"> <span aria-hidden="true" />有未保存修改 </span>
        <div class="agent-settings-dirty-bar-actions">
          <button
            type="button"
            class="admin-btn agent-settings-action agent-settings-action-discard"
            :disabled="saving"
            @click="restoreSettingsDraft"
          >
            <CloseOutlined aria-hidden="true" />
            <span>放弃修改</span>
          </button>
          <button
            type="button"
            class="admin-btn agent-settings-action agent-settings-action-save"
            :disabled="!canSave"
            @click="saveSettings"
          >
            <SaveOutlined aria-hidden="true" />
            <span>{{ saving ? '保存中...' : '保存设置' }}</span>
          </button>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style lang="scss">
.agent-settings-page {
  width: 100%;
}
.agent-settings-page-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 4px;
  padding-bottom: 16px;
}
.agent-settings-page-heading-copy {
  min-width: 0;
}
.agent-settings-page-heading h1 {
  margin: 0 0 5px;
  color: var(--text-primary);
  font-size: var(--marvo-type-20);
}
.agent-settings-page-heading p {
  margin: 0;
  color: var(--text-secondary);
}
.agent-settings-body {
  display: block;
}
.agent-settings-form {
  display: block;
}
.agent-settings-sections {
  display: grid;
  gap: 16px;
}
.agent-settings-block {
  min-width: 0;
}
.agent-runtime-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: stretch;
  gap: 16px;
}
.agent-runtime-grid > * {
  min-width: 0;
}
.agent-runtime-grid > .provider-settings,
.agent-runtime-grid > .agent-settings-block,
.agent-runtime-grid .provider-list-section,
.agent-runtime-grid .agent-model-section {
  height: 100%;
  box-sizing: border-box;
}
.agent-settings-loading {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
  color: var(--text-muted);
  font-size: var(--marvo-type-13);
}
.agent-settings-loading .page-loading-spinner {
  position: static;
}
.agent-settings-section {
  padding: 22px 24px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: var(--bg-primary);
}
.agent-settings-section + .agent-settings-section {
  margin-top: 16px;
}
.agent-settings-section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.agent-settings-section-heading h2 {
  margin: 0 0 4px;
  color: var(--text-primary);
  font-size: var(--marvo-type-16);
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
.agent-exa-status {
  flex-shrink: 0;
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--bg-secondary);
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.agent-exa-status[data-state='已配置'] {
  background: color-mix(in srgb, #16a34a 11%, transparent);
  color: #15803d;
}
.agent-exa-status[data-state='等待保存'],
.agent-exa-status[data-state='等待替换'],
.agent-exa-status[data-state='等待移除'] {
  background: color-mix(in srgb, #d97706 12%, transparent);
  color: #b45309;
}
.agent-exa-status[data-state='不可用'] {
  background: color-mix(in srgb, var(--text-danger) 10%, transparent);
  color: var(--text-danger);
}
:root[data-color-scheme='dark'] .agent-exa-status[data-state='已配置'] {
  color: #4ade80;
}
:root[data-color-scheme='dark'] .agent-exa-status[data-state='等待保存'],
:root[data-color-scheme='dark'] .agent-exa-status[data-state='等待替换'],
:root[data-color-scheme='dark'] .agent-exa-status[data-state='等待移除'] {
  color: #fbbf24;
}
.agent-exa-layout {
  display: grid;
  grid-template-columns: minmax(300px, 0.72fr) minmax(460px, 1.28fr);
  align-items: center;
  gap: 22px;
}
.agent-exa-summary {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 12px;
  padding: 15px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.agent-exa-icon {
  display: inline-flex;
  width: 36px;
  height: 36px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, var(--bg-primary));
  color: var(--text-accent);
  font-size: var(--marvo-type-17);
}
.agent-exa-summary strong {
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
}
.agent-exa-summary p {
  margin: 4px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.6;
}
.agent-exa-field {
  min-width: 0;
}
.agent-exa-control {
  display: flex;
  align-items: stretch;
  gap: 8px;
}
.agent-exa-input {
  min-width: 0;
  height: 40px;
  flex: 1;
  box-sizing: border-box;
  padding: 0 12px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.agent-exa-input::placeholder {
  color: var(--text-muted);
}
.agent-exa-input:focus {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}
.agent-exa-input:disabled {
  background: var(--bg-secondary);
  color: var(--text-muted);
}
.agent-exa-remove {
  min-width: 106px;
  height: 40px;
  justify-content: center;
  gap: 6px;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  background: var(--bg-primary);
  color: var(--text-secondary);
}
.agent-exa-remove:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--text-danger) 42%, var(--border-primary));
  color: var(--text-danger);
}
.agent-exa-remove:disabled {
  cursor: default;
  opacity: 0.55;
}
.agent-exa-help {
  margin-top: 7px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.55;
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
  margin: 12px 0 0;
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
.agent-model-layout {
  display: grid;
  grid-template-columns: minmax(360px, 0.9fr) minmax(440px, 1.1fr);
  align-items: start;
  gap: 18px;
}
.agent-model-picker-column,
.agent-model-configuration {
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.agent-model-picker-hint,
.agent-model-configuration-empty {
  margin: 9px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
  line-height: 1.55;
}
.agent-model-configuration-heading {
  margin-bottom: 9px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  font-weight: 600;
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
  margin-top: 0;
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
.agent-variant-scroll {
  max-width: 100%;
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
.agent-settings-body [data-part='error-text'] {
  color: var(--text-danger);
}
.agent-advanced-grid {
  display: grid;
  grid-template-columns: minmax(420px, 0.88fr) minmax(520px, 1.12fr);
  align-items: stretch;
  gap: 16px;
}
.agent-advanced-grid > .agent-settings-section {
  height: 100%;
  box-sizing: border-box;
}
.agent-advanced-grid .agent-settings-section + .agent-settings-section {
  margin-top: 0;
}
.agent-settings-body [data-part='error-text'] {
  margin-top: 5px;
  font-size: var(--marvo-type-11);
}
.agent-settings-page-actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
  padding-top: 2px;
}
.agent-settings-unsaved-state {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 7px;
  color: color-mix(in srgb, #d97706 86%, var(--text-primary));
  font-size: var(--marvo-type-12);
  font-weight: 600;
  white-space: nowrap;
}
.agent-settings-unsaved-state > span {
  width: 8px;
  height: 8px;
  flex: none;
  border-radius: 50%;
  background: #f59e0b;
  box-shadow: 0 0 0 3px color-mix(in srgb, #f59e0b 16%, transparent);
}
.agent-settings-action {
  width: 112px;
  height: 34px;
  justify-content: center;
  gap: 7px;
  border: 1px solid;
  border-radius: 8px;
  font-weight: 500;
  box-shadow: 0 1px 2px color-mix(in srgb, var(--text-primary) 10%, transparent);
  transition:
    background 0.15s,
    border-color 0.15s,
    color 0.15s,
    box-shadow 0.15s,
    transform 0.12s;
}
.agent-settings-action > .anticon {
  width: 16px;
  height: 16px;
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  font-size: var(--marvo-type-14);
}
.agent-settings-action-discard {
  border-color: color-mix(in srgb, var(--text-primary) 14%, var(--bg-primary));
  background: color-mix(in srgb, var(--text-primary) 6%, var(--bg-primary));
  color: var(--text-primary);
}
.agent-settings-action-discard:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--text-primary) 24%, var(--bg-primary));
  background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-primary));
}
.agent-settings-action-save {
  border-color: var(--marvo-accent-color, #4f46e5);
  background: var(--marvo-accent-color, #4f46e5);
  color: #fff;
}
.agent-settings-action-save:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 88%, #000);
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 88%, #000);
}
.agent-settings-action:active:not(:disabled) {
  transform: translateY(1px);
}
.agent-settings-action:disabled {
  cursor: default;
  box-shadow: none;
}
.agent-settings-action-discard:disabled {
  border-color: var(--border-primary);
  background: var(--bg-secondary);
  color: var(--text-muted);
}
.agent-settings-action-save:disabled {
  border-color: var(--border-primary);
  background: var(--bg-secondary);
  color: var(--text-muted);
}
.agent-settings-dirty-bar {
  position: fixed;
  right: max(18px, env(safe-area-inset-right));
  bottom: max(18px, env(safe-area-inset-bottom));
  z-index: 80;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 11px 10px 15px;
  border: 1px solid var(--border-primary);
  border-radius: 12px;
  background: color-mix(in srgb, var(--bg-card) 96%, transparent);
  box-shadow: 0 12px 36px color-mix(in srgb, var(--text-primary) 16%, transparent);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
}
.agent-settings-dirty-bar-actions {
  display: flex;
  gap: 8px;
}
.agent-settings-dirty-bar-enter-active,
.agent-settings-dirty-bar-leave-active {
  transition:
    opacity 0.16s,
    transform 0.16s;
}
.agent-settings-dirty-bar-enter-from,
.agent-settings-dirty-bar-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
.agent-settings-visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
@media (max-width: 600px), (max-width: 900px) and (orientation: portrait) {
  .agent-settings-page-heading {
    flex-direction: column;
    gap: 14px;
  }
  .agent-settings-page-actions {
    align-self: stretch;
    justify-content: flex-end;
  }
  .agent-settings-page-actions .agent-settings-unsaved-state {
    margin-right: auto;
  }
  .agent-settings-dirty-bar {
    right: max(10px, env(safe-area-inset-right));
    bottom: max(10px, env(safe-area-inset-bottom));
    left: max(10px, env(safe-area-inset-left));
    justify-content: space-between;
    gap: 10px;
    padding-left: 12px;
  }
  .agent-settings-dirty-bar-actions {
    min-width: 0;
  }
  .agent-settings-dirty-bar .agent-settings-action {
    width: auto;
    min-width: 96px;
  }
  .agent-settings-section {
    padding-inline: 16px;
  }
  .agent-settings-prompt-meta {
    align-items: flex-end;
    flex-direction: column;
    gap: 3px;
  }
  .agent-exa-control {
    align-items: stretch;
    flex-direction: column;
  }
  .agent-exa-input {
    width: 100%;
    min-height: 40px;
    flex: 0 0 40px;
  }
  .agent-exa-remove {
    align-self: flex-end;
  }
  .agent-personalization-delete {
    min-height: 40px;
  }
  .agent-variant-scroll {
    overflow-x: auto;
    padding-bottom: 4px;
    overscroll-behavior-x: contain;
    touch-action: pan-x;
  }
  .agent-variant-group {
    width: max-content;
    max-width: none;
    flex-wrap: nowrap;
  }
  .agent-variant-item {
    display: inline-flex;
    min-width: 64px;
    min-height: 40px;
    flex: 0 0 auto;
    align-items: center;
    justify-content: center;
    padding-inline: 12px;
    white-space: nowrap;
  }
}

@media (max-width: 1200px) {
  .agent-model-layout,
  .agent-exa-layout,
  .agent-advanced-grid {
    grid-template-columns: 1fr;
  }
}

@media (min-width: 2100px) {
  .agent-runtime-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
