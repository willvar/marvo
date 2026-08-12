<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Combobox, useListCollection } from '@ark-ui/vue/combobox'
import { Dialog } from '@ark-ui/vue/dialog'
import { Field } from '@ark-ui/vue/field'
import { RadioGroup } from '@ark-ui/vue/radio-group'
import { Select, createListCollection, type ListCollection } from '@ark-ui/vue/select'
import {
  ApiOutlined,
  CheckOutlined,
  CheckCircleOutlined,
  CloseOutlined,
  DeleteOutlined,
  DownOutlined,
  LinkOutlined,
  ReloadOutlined,
} from '@ant-design/icons-vue'
import {
  api,
  type AgentProvider,
  type AgentProviderMethod,
  type AgentProviderOAuthAttempt,
  type AgentProviderPromptOption,
} from '../sdk'
import XActionsCopy from './x/XActionsCopy.vue'

const props = defineProps<{ active: boolean }>()
const emit = defineEmits<{ changed: [] }>()

const ATTEMPT_STORAGE_KEY = 'marvo.agent.provider-oauth-attempt'

const providers = ref<AgentProvider[]>([])
const loading = ref(false)
const operating = ref(false)
const error = ref('')
const notice = ref('')
const selectedProviderID = ref('')
const providerValues = ref<string[]>([])
const selectedMethodIndex = ref('')
const methodInputs = ref<Record<string, string>>({})
const apiKey = ref('')
const attempt = ref<AgentProviderOAuthAttempt | null>(null)
const authorizationCode = ref('')
const disconnectTarget = ref<AgentProvider | null>(null)
let pollingTimer: ReturnType<typeof setTimeout> | undefined
let loadSequence = 0

const selectedProvider = computed(
  () => providers.value.find((provider) => provider.id === selectedProviderID.value) || null,
)
const availableMethods = computed(() => selectedProvider.value?.methods.filter((method) => method.available) || [])
const selectedMethod = computed(
  () =>
    availableMethods.value.find((method) => String(method.index) === selectedMethodIndex.value) ||
    availableMethods.value[0] ||
    null,
)
const connectedProviders = computed(() =>
  providers.value.filter((provider) => provider.connected).sort(compareProviders),
)
const providerOptions = computed(() => {
  return providers.value.filter((provider) => !provider.connected).sort(compareProviders)
})

function compareProviders(left: AgentProvider, right: AgentProvider) {
  const nameOrder = left.name.localeCompare(right.name)
  return nameOrder || left.id.localeCompare(right.id)
}
const {
  collection: providerCollection,
  filter: filterProviders,
  set: setProviderItems,
} = useListCollection<AgentProvider>({
  initialItems: [],
  itemToValue: (provider) => provider.id,
  itemToString: (provider) => `${provider.name} · ${provider.id}`,
  filter: (_itemText, query, provider) => {
    const haystack = `${provider.name} ${provider.id}`.toLocaleLowerCase()
    return haystack.includes(query.trim().toLocaleLowerCase())
  },
})
const visiblePrompts = computed(() =>
  (selectedMethod.value?.prompts || []).filter((prompt) => promptVisible(prompt.when)),
)
const promptCollections = computed(() => {
  const collections = new Map<string, ListCollection<AgentProviderPromptOption>>()
  for (const prompt of selectedMethod.value?.prompts || []) {
    if (prompt.type !== 'select') continue
    collections.set(
      prompt.key,
      createListCollection({
        items: prompt.options || [],
        itemToValue: (item) => item.value,
        itemToString: (item) => item.label,
      }),
    )
  }
  return collections
})
const canConnect = computed(() => {
  const method = selectedMethod.value
  if (!method || !method.available || operating.value) return false
  if (method.type === 'api' && !apiKey.value.trim()) return false
  return visiblePrompts.value.every((prompt) => {
    const value = methodInputs.value[prompt.key]?.trim() || ''
    return !!value || prompt.message.toLocaleLowerCase().includes('optional')
  })
})
const attemptPending = computed(() => attempt.value?.status === 'pending')

watch(
  () => props.active,
  (active) => {
    if (!active) {
      stopPolling()
      return
    }
    void loadProviders().then(resumeStoredAttempt)
  },
  { immediate: true },
)

watch(selectedMethodIndex, resetMethodFields)

watch(
  providerValues,
  (values) => {
    const provider = providers.value.find((candidate) => candidate.id === values[0])
    if (!provider) return
    selectProvider(provider)
  },
  { flush: 'sync' },
)

async function loadProviders() {
  const sequence = ++loadSequence
  loading.value = true
  error.value = ''
  try {
    const { data } = await api.get('/api/agent/providers')
    if (sequence !== loadSequence) return
    providers.value = Array.isArray(data?.providers) ? data.providers : []
    setProviderItems(providerOptions.value)
    const selected = providers.value.find((provider) => provider.id === selectedProviderID.value)
    if (selectedProviderID.value && !selected) {
      selectedProviderID.value = ''
      providerValues.value = []
    } else if (selected?.connected) {
      selectedProviderID.value = ''
      selectedMethodIndex.value = ''
      providerValues.value = []
    }
  } catch (cause) {
    if (sequence !== loadSequence) return
    error.value = errorMessage(cause, '读取提供商失败')
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

function selectProvider(provider: AgentProvider) {
  if (attempt.value && attempt.value.provider_id !== provider.id) clearAttempt()
  selectedProviderID.value = provider.id
  const nextValues = provider.connected ? [] : [provider.id]
  if (providerValues.value[0] !== nextValues[0] || providerValues.value.length !== nextValues.length) {
    providerValues.value = nextValues
  }
  const method = provider.methods.find((candidate) => candidate.available)
  selectedMethodIndex.value = method ? String(method.index) : ''
  resetMethodFields()
  error.value = ''
  notice.value = ''
}

function handleProviderComboboxOpen(details: { open: boolean }) {
  if (details.open) setProviderItems(providerOptions.value)
}

function resetMethodFields() {
  const method = selectedMethod.value
  const inputs: Record<string, string> = {}
  for (const prompt of method?.prompts || []) {
    inputs[prompt.key] = prompt.type === 'select' ? prompt.options?.[0]?.value || '' : ''
  }
  methodInputs.value = inputs
  apiKey.value = ''
  error.value = ''
}

function promptVisible(when: AgentProviderMethod['prompts'][number]['when']) {
  if (!when) return true
  const actual = methodInputs.value[when.key] || ''
  if (['neq', 'not_equals', '!='].includes(when.op.toLocaleLowerCase())) return actual !== when.value
  return actual === when.value
}

function promptCollection(key: string) {
  return promptCollections.value.get(key) || createListCollection<AgentProviderPromptOption>({ items: [] })
}

function updatePromptSelection(key: string, value: string[]) {
  methodInputs.value[key] = value[0] || ''
}

async function connectSelected() {
  const provider = selectedProvider.value
  const method = selectedMethod.value
  if (!provider || !method || !canConnect.value) return
  operating.value = true
  error.value = ''
  notice.value = ''
  try {
    if (method.type === 'api') {
      await api.post(`/api/agent/providers/${encodeURIComponent(provider.id)}/connect/key`, {
        method_index: method.index,
        key: apiKey.value,
        inputs: methodInputs.value,
      })
      apiKey.value = ''
      notice.value = `${provider.name} 已连接。`
      await loadProviders()
      emit('changed')
      return
    }
    const { data } = await api.post(`/api/agent/providers/${encodeURIComponent(provider.id)}/connect/oauth`, {
      method_index: method.index,
      inputs: methodInputs.value,
    })
    attempt.value = data as AgentProviderOAuthAttempt
    authorizationCode.value = ''
    sessionStorage.setItem(ATTEMPT_STORAGE_KEY, attempt.value.id)
    schedulePolling()
  } catch (cause) {
    error.value = errorMessage(cause, '连接提供商失败')
  } finally {
    operating.value = false
  }
}

async function completeAuthorization() {
  if (!attempt.value || attempt.value.mode !== 'code' || !authorizationCode.value.trim() || operating.value) return
  operating.value = true
  error.value = ''
  try {
    const { data } = await api.post(`/api/agent/provider-attempts/${encodeURIComponent(attempt.value.id)}/complete`, {
      code: authorizationCode.value,
    })
    applyAttempt(data as AgentProviderOAuthAttempt)
  } catch (cause) {
    error.value = errorMessage(cause, '提交授权码失败')
    await pollAttempt()
  } finally {
    operating.value = false
  }
}

async function pollAttempt() {
  const current = attempt.value
  if (!current || current.status !== 'pending' || !props.active) return
  try {
    const { data } = await api.get(`/api/agent/provider-attempts/${encodeURIComponent(current.id)}`)
    if (attempt.value?.id !== current.id) return
    applyAttempt(data as AgentProviderOAuthAttempt)
  } catch (cause: any) {
    if (cause?.status === 404) {
      clearAttempt()
      error.value = '授权已结束，请重新连接。'
      return
    }
  }
  if (attempt.value?.status === 'pending') schedulePolling()
}

function applyAttempt(value: AgentProviderOAuthAttempt) {
  const previous = attempt.value?.status
  attempt.value = value
  if (value.status === 'pending') {
    sessionStorage.setItem(ATTEMPT_STORAGE_KEY, value.id)
    return
  }
  stopPolling()
  sessionStorage.removeItem(ATTEMPT_STORAGE_KEY)
  if (value.status === 'succeeded' && previous !== 'succeeded') {
    notice.value = `${value.provider_name} 已连接。`
    emit('changed')
  }
  if (value.status === 'succeeded') {
    const attemptID = value.id
    void loadProviders().finally(() => {
      if (attempt.value?.id === attemptID) clearAttempt()
    })
  }
}

function schedulePolling() {
  stopPolling()
  if (!props.active || !attemptPending.value) return
  pollingTimer = setTimeout(() => void pollAttempt(), 1_000)
}

function stopPolling() {
  if (!pollingTimer) return
  clearTimeout(pollingTimer)
  pollingTimer = undefined
}

async function resumeStoredAttempt() {
  if (attempt.value?.status === 'pending') {
    schedulePolling()
    return
  }
  const attemptID = sessionStorage.getItem(ATTEMPT_STORAGE_KEY)
  if (!attemptID) return
  try {
    const { data } = await api.get(`/api/agent/provider-attempts/${encodeURIComponent(attemptID)}`)
    attempt.value = data as AgentProviderOAuthAttempt
    const provider = providers.value.find((candidate) => candidate.id === attempt.value?.provider_id)
    if (provider) selectProvider(provider)
    if (attempt.value.status === 'pending') schedulePolling()
    else applyAttempt(attempt.value)
  } catch {
    sessionStorage.removeItem(ATTEMPT_STORAGE_KEY)
  }
}

async function cancelAuthorization() {
  if (!attempt.value || operating.value) return
  operating.value = true
  try {
    await api.delete(`/api/agent/provider-attempts/${encodeURIComponent(attempt.value.id)}`)
    clearAttempt()
  } catch (cause) {
    error.value = errorMessage(cause, '取消授权失败')
  } finally {
    operating.value = false
  }
}

function clearAttempt() {
  stopPolling()
  attempt.value = null
  authorizationCode.value = ''
  sessionStorage.removeItem(ATTEMPT_STORAGE_KEY)
}

function dismissAttempt() {
  clearAttempt()
}

async function confirmDisconnect() {
  const provider = disconnectTarget.value
  if (!provider || operating.value) return
  operating.value = true
  error.value = ''
  try {
    await api.delete(`/api/agent/providers/${encodeURIComponent(provider.id)}`)
    disconnectTarget.value = null
    notice.value = `${provider.name} 已断开。`
    await loadProviders()
    emit('changed')
  } catch (cause) {
    error.value = errorMessage(cause, '断开提供商失败')
    disconnectTarget.value = null
  } finally {
    operating.value = false
  }
}

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error && cause.message ? cause.message : fallback
}

function methodTypeLabel(method: AgentProviderMethod) {
  return method.type === 'oauth' ? '跳转授权' : '粘贴密钥'
}

onBeforeUnmount(stopPolling)
</script>

<template>
  <div class="provider-settings">
    <div v-if="loading && providers.length === 0" class="provider-loading" aria-label="正在读取提供商">
      <span class="page-loading-spinner" />
      <span>正在读取 OpenCode 提供商...</span>
    </div>

    <template v-else>
      <section class="provider-list-section">
        <div class="provider-list-heading">
          <div>
            <h4>提供商</h4>
            <p>连接和断开操作立即生效，模型列表会自动刷新，无需另行保存。</p>
          </div>
          <button type="button" class="provider-refresh" :disabled="loading" @click="loadProviders">
            <ReloadOutlined aria-hidden="true" />
            <span>{{ loading ? '刷新中' : '刷新' }}</span>
          </button>
        </div>

        <div v-if="notice" class="provider-inline-success" role="status">{{ notice }}</div>
        <div v-if="error" class="provider-inline-error" role="alert">{{ error }}</div>

        <section class="provider-connected-section">
          <div class="provider-subsection-heading">
            <div>
              <h5>已连接提供商</h5>
              <p>选择一个提供商可查看连接状态或断开凭据。</p>
            </div>
            <span>{{ connectedProviders.length }} 个</span>
          </div>

          <div v-if="connectedProviders.length" class="provider-connected-list">
            <div v-for="provider in connectedProviders" :key="provider.id" class="provider-connected-item">
              <span class="provider-logo"><ApiOutlined aria-hidden="true" /></span>
              <span class="provider-connected-item-main">
                <strong>{{ provider.name }}</strong>
                <span>{{ provider.id }} · {{ provider.model_count }} 个模型</span>
              </span>
              <span v-if="!provider.can_disconnect" class="provider-status connected">环境管理</span>
              <button
                v-if="provider.can_disconnect"
                type="button"
                class="admin-btn provider-connected-disconnect"
                :disabled="operating || attemptPending"
                :aria-label="`断开 ${provider.name}`"
                @click="disconnectTarget = provider"
              >
                <DeleteOutlined aria-hidden="true" />
                <span>断开</span>
              </button>
            </div>
          </div>
          <p v-else class="provider-empty provider-connected-empty">尚未连接提供商。</p>
        </section>

        <div class="provider-picker-heading">
          <span>连接新提供商</span>
          <span>{{ providerOptions.length }} 个可选</span>
        </div>
        <Combobox.Root
          v-model="providerValues"
          :collection="providerCollection"
          :disabled="providerOptions.length === 0 || operating || attemptPending"
          :positioning="{ placement: 'bottom-start', sameWidth: true }"
          input-behavior="autohighlight"
          open-on-click
          @input-value-change="filterProviders($event.inputValue)"
          @open-change="handleProviderComboboxOpen"
        >
          <Combobox.Label class="provider-picker-label">提供商</Combobox.Label>
          <Combobox.Control class="provider-picker-control">
            <Combobox.Input class="provider-picker-input" placeholder="搜索提供商名称或 ID" aria-label="选择提供商" />
            <Combobox.Trigger class="provider-picker-trigger" aria-label="展开提供商列表">
              <DownOutlined aria-hidden="true" />
            </Combobox.Trigger>
          </Combobox.Control>
          <Teleport to="body">
            <Combobox.Positioner class="provider-picker-positioner">
              <Combobox.Content class="provider-picker-content">
                <Combobox.Empty class="provider-picker-empty">没有匹配的未连接提供商</Combobox.Empty>
                <Combobox.Item
                  v-for="provider in providerCollection.items"
                  :key="provider.id"
                  :item="provider"
                  class="provider-picker-item"
                >
                  <span class="provider-logo"><ApiOutlined aria-hidden="true" /></span>
                  <span class="provider-picker-item-main">
                    <span class="provider-picker-name-line">
                      <Combobox.ItemText class="provider-picker-name">{{ provider.name }}</Combobox.ItemText>
                    </span>
                    <span class="provider-picker-meta">{{ provider.id }} · {{ provider.model_count }} 个模型</span>
                  </span>
                </Combobox.Item>
              </Combobox.Content>
            </Combobox.Positioner>
          </Teleport>
        </Combobox.Root>
        <p v-if="providers.length === 0" class="provider-empty">OpenCode 当前没有可配置的提供商。</p>
        <p v-else-if="providerOptions.length === 0" class="provider-empty">所有可用提供商均已连接。</p>

        <section v-if="attempt" class="provider-detail provider-oauth">
          <div class="provider-oauth-card" :data-status="attempt.status">
            <span class="provider-oauth-icon">
              <CheckCircleOutlined v-if="attempt.status === 'succeeded'" aria-hidden="true" />
              <CloseOutlined v-else-if="attempt.status !== 'pending'" aria-hidden="true" />
              <span v-else class="page-loading-spinner" />
            </span>
            <div>
              <h4>
                {{
                  attempt.status === 'succeeded'
                    ? '连接成功'
                    : attempt.status === 'pending'
                      ? '等待完成授权'
                      : '授权未完成'
                }}
              </h4>
              <p>{{ attempt.provider_name }} · {{ attempt.method_label }}</p>
            </div>
          </div>

          <template v-if="attempt.status === 'pending'">
            <p v-if="attempt.instructions" class="provider-oauth-instructions">{{ attempt.instructions }}</p>
            <div v-if="attempt.code" class="provider-verification-code">
              <div>
                <span>确认代码</span>
                <strong>{{ attempt.code }}</strong>
              </div>
              <XActionsCopy :text="attempt.code" />
            </div>

            <a
              class="admin-btn admin-btn-primary provider-external-link"
              :href="attempt.url"
              target="_blank"
              rel="noopener noreferrer"
            >
              <LinkOutlined aria-hidden="true" />
              <span>打开授权页面</span>
            </a>

            <Field.Root v-if="attempt.mode === 'code'" class="provider-field">
              <Field.Label>授权码</Field.Label>
              <Field.Input
                v-model="authorizationCode"
                autocomplete="off"
                placeholder="粘贴授权页面返回的代码"
                @keydown.enter.prevent.stop="completeAuthorization"
              />
            </Field.Root>

            <div class="provider-detail-actions">
              <button
                v-if="attempt.mode === 'code'"
                type="button"
                class="admin-btn admin-btn-primary"
                :disabled="operating || !authorizationCode.trim()"
                @click="completeAuthorization"
              >
                <CheckCircleOutlined aria-hidden="true" />
                <span>{{ operating ? '正在验证...' : '完成连接' }}</span>
              </button>
              <button type="button" class="admin-btn" :disabled="operating" @click="cancelAuthorization">
                <CloseOutlined aria-hidden="true" />
                <span>取消授权</span>
              </button>
            </div>
          </template>

          <template v-else-if="attempt.status !== 'succeeded'">
            <p v-if="attempt.error" class="provider-inline-error" role="alert">{{ attempt.error }}</p>
            <button type="button" class="admin-btn admin-btn-primary provider-retry-button" @click="dismissAttempt">
              <ReloadOutlined aria-hidden="true" />
              <span>重新连接</span>
            </button>
          </template>
        </section>

        <section v-else-if="selectedProvider && !selectedProvider.connected" class="provider-detail">
          <div class="provider-detail-title">
            <span class="provider-logo"><ApiOutlined aria-hidden="true" /></span>
            <div>
              <h4>{{ selectedProvider.name }}</h4>
              <p>{{ selectedProvider.id }} · {{ selectedProvider.model_count }} 个模型</p>
            </div>
          </div>

          <RadioGroup.Root
            v-if="availableMethods.length > 1"
            v-model="selectedMethodIndex"
            class="provider-methods"
            aria-label="连接方式"
          >
            <RadioGroup.Item
              v-for="method in availableMethods"
              :key="method.index"
              class="provider-method"
              :value="String(method.index)"
            >
              <RadioGroup.ItemHiddenInput />
              <RadioGroup.ItemControl><span /></RadioGroup.ItemControl>
              <RadioGroup.ItemText>
                <strong>{{ method.label }}</strong>
                <span>{{ methodTypeLabel(method) }}</span>
              </RadioGroup.ItemText>
            </RadioGroup.Item>
          </RadioGroup.Root>

          <div v-if="selectedMethod" class="provider-credentials">
            <template v-for="prompt in visiblePrompts" :key="prompt.key">
              <Field.Root v-if="prompt.type === 'text'" class="provider-field">
                <Field.Label>{{ prompt.message || prompt.key }}</Field.Label>
                <Field.Input
                  v-model="methodInputs[prompt.key]"
                  :placeholder="prompt.placeholder"
                  autocomplete="off"
                  @keydown.enter.prevent.stop="connectSelected"
                />
              </Field.Root>
              <Select.Root
                v-else
                class="provider-field"
                :collection="promptCollection(prompt.key)"
                :model-value="methodInputs[prompt.key] ? [methodInputs[prompt.key]] : []"
                :positioning="{ placement: 'bottom-start', sameWidth: true }"
                @value-change="updatePromptSelection(prompt.key, $event.value)"
              >
                <Select.HiddenSelect />
                <Select.Label>{{ prompt.message || prompt.key }}</Select.Label>
                <Select.Control>
                  <Select.Trigger class="provider-select-trigger">
                    <Select.ValueText :placeholder="prompt.placeholder || '请选择'" />
                    <Select.Indicator><DownOutlined aria-hidden="true" /></Select.Indicator>
                  </Select.Trigger>
                </Select.Control>
                <Teleport to="body">
                  <Select.Positioner class="provider-select-positioner">
                    <Select.Content class="provider-select-content">
                      <Select.Item
                        v-for="option in prompt.options"
                        :key="option.value"
                        :item="option"
                        class="provider-select-item"
                      >
                        <Select.ItemText>
                          <strong>{{ option.label }}</strong>
                          <span v-if="option.hint">{{ option.hint }}</span>
                        </Select.ItemText>
                        <Select.ItemIndicator><CheckOutlined aria-hidden="true" /></Select.ItemIndicator>
                      </Select.Item>
                    </Select.Content>
                  </Select.Positioner>
                </Teleport>
              </Select.Root>
            </template>

            <Field.Root v-if="selectedMethod.type === 'api'" class="provider-field">
              <Field.Label>{{ availableMethods.length > 1 ? '密钥' : 'API Key' }}</Field.Label>
              <Field.Input
                v-model="apiKey"
                type="password"
                autocomplete="off"
                placeholder="输入密钥"
                @keydown.enter.prevent.stop="connectSelected"
              />
              <Field.HelperText>只发送到 Marvo 后端并交由 OpenCode 保存。</Field.HelperText>
            </Field.Root>

            <button
              type="button"
              class="admin-btn admin-btn-primary provider-connect-button"
              :disabled="!canConnect"
              @click="connectSelected"
            >
              <LinkOutlined v-if="selectedMethod.type === 'oauth'" aria-hidden="true" />
              <ApiOutlined v-else aria-hidden="true" />
              <span>{{ operating ? '连接中...' : selectedMethod.type === 'oauth' ? '开始授权' : '连接提供商' }}</span>
            </button>
          </div>
          <p v-else class="provider-inline-error" role="status">当前版本暂不支持连接此提供商。</p>
        </section>
      </section>
    </template>

    <Dialog.Root
      :open="!!disconnectTarget"
      lazy-mount
      unmount-on-exit
      @update:open="!$event && !operating && (disconnectTarget = null)"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop provider-confirm-backdrop" />
        <Dialog.Positioner class="dialog-positioner provider-confirm-positioner">
          <Dialog.Content class="dialog-panel provider-confirm-dialog">
            <div class="dialog-header">
              <div>
                <Dialog.Title>断开提供商</Dialog.Title>
                <Dialog.Description>
                  将从 OpenCode 删除 {{ disconnectTarget?.name }} 的连接凭据。之后可重新连接。
                </Dialog.Description>
              </div>
              <Dialog.CloseTrigger class="dialog-close" aria-label="关闭确认" :disabled="operating">
                <CloseOutlined />
              </Dialog.CloseTrigger>
            </div>
            <div class="dialog-actions">
              <Dialog.CloseTrigger class="admin-btn" :disabled="operating">
                <CloseOutlined aria-hidden="true" />
                <span>取消</span>
              </Dialog.CloseTrigger>
              <button type="button" class="admin-btn admin-btn-danger" :disabled="operating" @click="confirmDisconnect">
                <DeleteOutlined aria-hidden="true" />
                <span>{{ operating ? '正在断开...' : '确认断开' }}</span>
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>
  </div>
</template>

<style lang="scss" scoped>
.provider-settings {
  min-height: 100%;
  color: var(--text-primary);
}
.provider-loading {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--text-muted);
  font-size: var(--marvo-type-13);
}
.provider-loading .page-loading-spinner,
.provider-oauth-icon .page-loading-spinner {
  position: static;
}
.provider-list-section {
  padding: 22px 24px;
}
.provider-list-heading,
.provider-detail-title,
.provider-picker-heading,
.provider-picker-control,
.provider-picker-item,
.provider-picker-name-line,
.provider-verification-code,
.provider-oauth-card {
  display: flex;
  align-items: center;
}
.provider-list-heading,
.provider-picker-heading,
.provider-verification-code {
  justify-content: space-between;
}
.provider-list-heading {
  align-items: flex-start;
  gap: 16px;
}
.provider-subsection-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.provider-subsection-heading h5 {
  margin: 0 0 4px;
  color: var(--text-primary);
  font-size: var(--marvo-type-13);
}
.provider-subsection-heading p {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.provider-subsection-heading > span {
  flex: 0 0 auto;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.provider-list-heading h4,
.provider-detail-title h4,
.provider-oauth-card h4 {
  margin: 0 0 4px;
  font-size: var(--marvo-type-14);
}
.provider-list-heading p,
.provider-detail-title p,
.provider-oauth-card p {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}
.provider-refresh {
  display: inline-flex;
  min-height: 32px;
  align-items: center;
  gap: 6px;
  padding: 0 8px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-12);
}
.provider-refresh:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.provider-refresh:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.provider-picker-heading {
  gap: 12px;
  margin: 22px 0 9px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  font-weight: 600;
}
.provider-picker-heading span:last-child {
  color: var(--text-muted);
  font-weight: 400;
}
.provider-connected-section {
  margin-top: 22px;
}
.provider-connected-list {
  display: grid;
  gap: 8px;
  margin-top: 10px;
}
.provider-connected-item {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
  padding: 10px;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-primary);
  text-align: left;
}
.provider-connected-item-main {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}
.provider-connected-item-main strong,
.provider-connected-item-main > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-connected-item-main strong {
  font-size: var(--marvo-type-12);
}
.provider-connected-item-main > span {
  color: var(--text-muted);
  font-size: var(--marvo-type-10);
}
.provider-connected-empty {
  margin-top: 10px;
}
.provider-connected-disconnect {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
  color: var(--text-danger);
}
.provider-picker-label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
.provider-picker-control {
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
.provider-picker-control:focus-within {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}
.provider-picker-control[data-disabled] {
  opacity: 0.55;
}
.provider-picker-input {
  min-width: 0;
  height: 100%;
  flex: 1;
  padding: 0 12px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
}
.provider-picker-input::placeholder {
  color: var(--text-muted);
}
.provider-picker-trigger {
  width: 40px;
  height: 100%;
  border: 0;
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
}
.provider-picker-positioner {
  z-index: 1300 !important;
}
.provider-picker-content {
  width: var(--reference-width);
  max-width: calc(100vw - 24px);
  max-height: min(390px, 55vh);
  overflow-y: auto;
  padding: 5px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  outline: 0;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}
.provider-picker-item {
  gap: 10px;
  padding: 9px 10px;
  border-radius: 7px;
  outline: 0;
  color: var(--text-primary);
  cursor: pointer;
}
.provider-picker-item[data-highlighted] {
  background: var(--bg-hover);
}
.provider-picker-empty {
  padding: 24px;
  color: var(--text-muted);
  text-align: center;
  font-size: var(--marvo-type-12);
}
.provider-logo {
  display: inline-flex;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, var(--bg-secondary));
  color: var(--text-accent);
}
.provider-picker-item-main {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}
.provider-picker-name-line {
  min-width: 0;
  gap: 7px;
}
.provider-picker-name,
.provider-picker-meta {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-picker-name {
  min-width: 0;
  font-size: var(--marvo-type-13);
  font-weight: 600;
}
.provider-picker-meta {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.provider-status {
  flex: 0 0 auto;
  color: var(--text-accent);
  font-size: var(--marvo-type-11);
}
.provider-status.connected {
  padding: 3px 6px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, transparent);
}
.provider-refresh:focus-visible,
.provider-picker-trigger:focus-visible {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--marvo-accent-color) 25%, transparent);
}
.provider-empty {
  margin: 10px 0 0;
  padding: 28px 12px;
  border: 1px dashed var(--border-primary);
  border-radius: 9px;
  color: var(--text-muted);
  text-align: center;
  font-size: var(--marvo-type-12);
}
.provider-detail {
  margin-top: 20px;
  padding-top: 18px;
  border-top: 1px solid var(--border-primary);
}
.provider-detail-title {
  gap: 12px;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--border-primary);
}
.provider-detail-title > div {
  min-width: 0;
  flex: 1;
}
.provider-methods {
  display: grid;
  gap: 8px;
  margin-top: 18px;
}
.provider-method {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  padding: 11px 12px;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  cursor: pointer;
  outline: 0;
}
.provider-method:hover,
.provider-method[data-state='checked'] {
  border-color: var(--text-accent);
  background: color-mix(in srgb, var(--marvo-accent-color) 6%, transparent);
}
.provider-method[data-disabled] {
  cursor: not-allowed;
  opacity: 0.55;
}
.provider-method [data-part='item-control'] {
  width: 16px;
  height: 16px;
  box-sizing: border-box;
  margin-top: 2px;
  border: 1px solid var(--border-light);
  border-radius: 50%;
}
.provider-method [data-part='item-control'][data-state='checked'] {
  border: 5px solid var(--marvo-accent-color);
}
.provider-method [data-part='item-text'] {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}
.provider-method strong {
  font-size: var(--marvo-type-12);
}
.provider-method [data-part='item-text'] span {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.provider-credentials {
  margin-top: 18px;
}
.provider-field + .provider-field {
  margin-top: 13px;
}
.provider-field [data-part='label'] {
  display: block;
  margin-bottom: 6px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  font-weight: 600;
}
.provider-field [data-part='input'],
.provider-select-trigger {
  width: 100%;
  height: 40px;
  box-sizing: border-box;
  padding: 0 11px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  outline: 0;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
}
.provider-field [data-part='input']:focus,
.provider-select-trigger:focus-visible {
  border-color: var(--text-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
}
.provider-select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
}
.provider-select-positioner {
  z-index: 1300 !important;
}
.provider-select-content {
  width: var(--reference-width);
  max-width: calc(100vw - 24px);
  max-height: min(300px, 45vh);
  overflow-y: auto;
  padding: 5px;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  outline: 0;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}
.provider-select-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 7px;
  outline: 0;
  color: var(--text-primary);
  cursor: pointer;
}
.provider-select-item[data-highlighted] {
  background: var(--bg-hover);
}
.provider-select-item [data-part='item-text'] {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}
.provider-select-item strong {
  font-size: var(--marvo-type-12);
}
.provider-select-item [data-part='item-text'] span {
  color: var(--text-muted);
  font-size: var(--marvo-type-10);
}
.provider-select-item [data-part='item-indicator'] {
  flex: 0 0 auto;
  color: var(--text-accent);
}
.provider-field [data-part='helper-text'] {
  margin-top: 5px;
  color: var(--text-muted);
  font-size: var(--marvo-type-10);
}
.provider-connect-button,
.provider-external-link {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-top: 16px;
  text-decoration: none;
}
.provider-oauth-card {
  align-items: flex-start;
  gap: 12px;
  margin-top: 18px;
  padding: 14px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.provider-oauth-icon {
  display: inline-flex;
  width: 30px;
  height: 30px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  color: var(--text-accent);
  font-size: var(--marvo-type-17);
}
.provider-oauth-card[data-status='failed'] .provider-oauth-icon,
.provider-oauth-card[data-status='expired'] .provider-oauth-icon,
.provider-oauth-card[data-status='cancelled'] .provider-oauth-icon {
  color: var(--text-danger);
}
.provider-oauth-instructions {
  margin: 16px 0 0;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  line-height: 1.6;
  white-space: pre-wrap;
}
.provider-verification-code {
  gap: 12px;
  margin-top: 14px;
  padding: 11px 12px;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  background: var(--bg-primary);
}
.provider-verification-code > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.provider-verification-code span {
  color: var(--text-muted);
  font-size: var(--marvo-type-10);
}
.provider-verification-code strong {
  letter-spacing: 0.08em;
  font-family: 'SF Mono', 'Fira Code', ui-monospace, monospace;
  font-size: var(--marvo-type-16);
}
.provider-oauth .provider-field {
  margin-top: 16px;
}
.provider-detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}
.provider-retry-button {
  margin-top: 14px;
}
.provider-inline-success,
.provider-inline-error {
  padding: 9px 11px;
  border-radius: 7px;
  font-size: var(--marvo-type-12);
  line-height: 1.5;
}
.provider-inline-success {
  margin-top: 14px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, transparent);
  color: var(--text-accent);
}
.provider-inline-error {
  background: color-mix(in srgb, var(--text-danger) 10%, transparent);
  color: var(--text-danger);
}
.provider-list-section > .provider-inline-error {
  margin-top: 14px;
}
.provider-credentials > .provider-inline-error,
.provider-oauth > .provider-inline-error {
  margin: 14px 0 0;
}
.provider-confirm-backdrop,
.provider-confirm-positioner {
  z-index: 1200;
}
.provider-confirm-dialog {
  max-width: 420px;
}
.provider-confirm-dialog [data-part='description'] {
  margin: 7px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
  line-height: 1.55;
}
.provider-confirm-dialog .dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 0 20px 20px;
}

@media (max-width: 600px) {
  .provider-list-section {
    padding-inline: 16px;
  }
  .provider-confirm-positioner {
    align-items: flex-end;
    padding: 0;
  }
  .provider-confirm-dialog {
    width: 100%;
    max-width: none;
    border-radius: 14px 14px 0 0;
  }
}
</style>
