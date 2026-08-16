<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Checkbox } from '@ark-ui/vue/checkbox'
import { Dialog } from '@ark-ui/vue/dialog'
import { Field } from '@ark-ui/vue/field'
import { Select, createListCollection, type ListCollection } from '@ark-ui/vue/select'
import { Toast, Toaster, createToaster } from '@ark-ui/vue/toast'
import {
  ApiOutlined,
  CheckCircleOutlined,
  CheckOutlined,
  CloseOutlined,
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  LoadingOutlined,
  NotificationOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
  SaveOutlined,
  SendOutlined,
  SwapOutlined,
} from '@ant-design/icons-vue'
import {
  buildCreateInput,
  buildTestInput,
  buildUpdateInput,
  filterProviders,
  groupProviders,
  isSensitiveField,
  secretIsPreserved as notixSecretIsPreserved,
  useConnectorCatalog,
  useConnectorForm,
  useConnectors,
  type ConnectorField,
  type ConnectorFieldOption,
  type ConnectorProvider,
} from '@willvar/notix-vue'
import { createConnectorClient, type ActivityConnector } from '../../sdk'
import { useRetainedDialog } from '../../composables/useRetainedDialog'
import { XButton, XPrompts } from '../../components/x'

interface EditorPayload {
  connector?: ActivityConnector
}

const emptyProvider: ConnectorProvider = {
  id: '',
  name: '',
  category: '',
  description: '',
  fields: [],
}
const connectorClient = createConnectorClient()
const catalogState = useConnectorCatalog(connectorClient)
const connectorsState = useConnectors(connectorClient)
const formController = useConnectorForm(emptyProvider)
const providers = computed(() => catalogState.providers.value)
const connectors = computed(() => connectorsState.connectors.value)
const form = formController.state
const loading = ref(true)
const loadError = ref('')
const editorDialog = useRetainedDialog<EditorPayload>()
const deleteDialog = useRetainedDialog<ActivityConnector>()
const { open: editorOpen, payload: editorPayload } = editorDialog
const { open: deleteOpen, payload: deleteTarget } = deleteDialog
const providerSearch = ref('')
const activeProviderCategory = ref('全部')
const editorError = ref('')
const saving = ref(false)
const testing = ref(false)
const deleting = ref(false)
const retryingID = ref('')
const testingID = ref('')
const deleteError = ref('')

const toaster = createToaster({
  placement: 'bottom',
  duration: 2600,
  max: 3,
  offsets: { top: '16px', right: '16px', bottom: '28px', left: '16px' },
})

const selectedProvider = computed(() => {
  return providers.value.find((provider) => provider.id === form.providerId) || null
})
const editing = computed(() => !!editorPayload.value?.connector)
const configuredCount = computed(() => connectors.value.filter((connector) => connector.enabled).length)
const providerGroups = computed(() => groupProviders(providers.value))
const providerCategories = computed(() => [...providerGroups.value.keys()])
const providerCategoryCounts = computed(
  () => new Map([...providerGroups.value].map(([category, items]) => [category, items.length])),
)
const visibleProviderGroups = computed(() => {
  let matches = filterProviders(providers.value, providerSearch.value)
  if (!providerSearch.value.trim() && activeProviderCategory.value !== '全部') {
    matches = matches.filter((provider) => provider.category === activeProviderCategory.value)
  }
  return [...groupProviders(matches)].map(([category, groupedProviders]) => ({
    category,
    providers: groupedProviders,
  }))
})
const visibleProviderCount = computed(() =>
  visibleProviderGroups.value.reduce((total, group) => total + group.providers.length, 0),
)
const selectCollections = computed(() => {
  const result = new Map<string, ListCollection<{ label: string; value: string }>>()
  for (const field of selectedProvider.value?.fields || []) {
    if (field.type !== 'select') continue
    const items = (field.options || []).map((option) => ({ label: option.label, value: String(option.value) }))
    result.set(
      field.key,
      createListCollection({ items, itemToValue: (item) => item.value, itemToString: (item) => item.label }),
    )
  }
  return result
})
const formValid = computed(() => !!selectedProvider.value && formController.valid.value)

watch(providerSearch, (value) => {
  if (value.trim()) activeProviderCategory.value = '全部'
})

onMounted(load)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    await Promise.all([catalogState.load(), connectorsState.load()])
  } catch (cause) {
    loadError.value = errorMessage(cause, '活动连接器加载失败')
  } finally {
    loading.value = false
  }
}

function showToast(title: string, type: 'success' | 'error') {
  toaster.create({ title, type })
}

function beginCreate() {
  providerSearch.value = ''
  activeProviderCategory.value = '全部'
  formController.reset(emptyProvider)
  editorError.value = ''
  editorDialog.show({})
}

function beginEdit(connector: ActivityConnector) {
  const provider = providers.value.find((candidate) => candidate.id === connector.provider_id)
  if (!provider) {
    showToast('对应的连接器服务已不可用', 'error')
    return
  }
  formController.reset(provider, connector)
  editorError.value = ''
  editorDialog.show({ connector })
}

function closeEditor() {
  if (saving.value || testing.value) return
  editorDialog.close()
}

function completeEditorClose() {
  if (!editorDialog.clearAfterExit()) return
  providerSearch.value = ''
  activeProviderCategory.value = '全部'
  formController.reset(emptyProvider)
  editorError.value = ''
}

function selectProvider(provider: ConnectorProvider) {
  formController.reset(provider)
  editorError.value = ''
}

function selectProviderByPrompt(prompt: { key: string }) {
  const provider = providers.value.find((candidate) => candidate.id === prompt.key)
  if (provider) selectProvider(provider)
}

function selectProviderCategory(category: string) {
  activeProviderCategory.value = category
  providerSearch.value = ''
}

function changeProvider() {
  formController.reset(emptyProvider)
  editorError.value = ''
}

function fieldCollection(field: ConnectorField) {
  return selectCollections.value.get(field.key) || createListCollection<{ label: string; value: string }>({ items: [] })
}

function updateSelect(field: ConnectorField, values: string[]) {
  const selected = field.options?.find((option) => String(option.value) === values[0])
  formController.setValue(field.key, selected?.value ?? '')
}

function updateBoolean(field: ConnectorField, checked: boolean | 'indeterminate') {
  formController.setValue(field.key, checked === true)
}

function updateField(field: ConnectorField, value: string) {
  formController.setValue(field.key, field.type === 'number' && value !== '' ? Number(value) : value)
}

function configuredSecret(field: ConnectorField) {
  return form.configuredSecrets[field.key] === true
}

function fieldSensitive(field: ConnectorField) {
  return isSensitiveField(field)
}

function secretIsPreserved(field: ConnectorField) {
  return notixSecretIsPreserved(form, field)
}

function toggleSecret(field: ConnectorField) {
  formController.clearSecret(field.key, !form.clearSecrets.includes(field.key))
}

async function save() {
  const provider = selectedProvider.value
  if (!provider || !formValid.value || saving.value) return
  saving.value = true
  editorError.value = ''
  try {
    const current = editorPayload.value?.connector
    if (current) {
      await connectorsState.update(current.id, buildUpdateInput(provider, form))
    } else {
      await connectorsState.create(buildCreateInput(provider, form))
    }
    formController.acceptCurrent()
    editorDialog.close()
    showToast('连接器已保存', 'success')
  } catch (cause) {
    editorError.value = errorMessage(cause, '保存连接器失败')
  } finally {
    saving.value = false
  }
}

async function testDraft() {
  const provider = selectedProvider.value
  if (!provider || !formValid.value || testing.value) return
  testing.value = true
  editorError.value = ''
  try {
    await connectorClient.testConnector(buildTestInput(provider, form, editorPayload.value?.connector?.id))
    showToast('测试消息已发送', 'success')
  } catch (cause) {
    editorError.value = errorMessage(cause, '测试发送失败')
  } finally {
    testing.value = false
  }
}

async function testSaved(connector: ActivityConnector) {
  if (testingID.value) return
  testingID.value = connector.id
  try {
    await connectorClient.testConnector({
      connector_id: connector.id,
      provider_id: connector.provider_id,
      config: {},
      clear_secrets: [],
    })
    showToast(`${connector.name} 测试消息已发送`, 'success')
  } catch (cause) {
    showToast(errorMessage(cause, '测试发送失败'), 'error')
  } finally {
    testingID.value = ''
  }
}

function requestDelete(connector: ActivityConnector) {
  deleteError.value = ''
  deleteDialog.show(connector)
}

async function confirmDelete() {
  const target = deleteTarget.value
  if (!target || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await connectorsState.remove(target.id)
    deleteDialog.close()
    showToast('连接器已删除', 'success')
  } catch (cause) {
    deleteError.value = errorMessage(cause, '删除连接器失败')
  } finally {
    deleting.value = false
  }
}

async function retryFailed(connector: ActivityConnector) {
  if (retryingID.value) return
  retryingID.value = connector.id
  try {
    const count = await connectorClient.retryConnector(connector.id)
    showToast(count ? `已重新排队 ${count} 条活动` : '没有需要重试的活动', 'success')
    await connectorsState.load()
  } catch (cause) {
    showToast(errorMessage(cause, '重新投递失败'), 'error')
  } finally {
    retryingID.value = ''
  }
}

function formatTime(value?: string) {
  if (!value) return '尚未发送'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '尚未发送'
  const two = (number: number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}.${two(date.getMonth() + 1)}.${two(date.getDate())} ${two(date.getHours())}:${two(date.getMinutes())}:${two(date.getSeconds())}`
}

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error && cause.message ? cause.message : fallback
}
</script>

<template>
  <section class="activity-connectors-page">
    <header class="activity-connectors-heading">
      <div>
        <h1>活动连接器</h1>
        <p>把智能体发布的新活动转发到通讯、推送、邮件或自动化服务。</p>
      </div>
      <XButton v-if="!loading && !loadError && connectors.length" variant="primary" @click="beginCreate">
        <PlusOutlined aria-hidden="true" />新建连接器
      </XButton>
    </header>

    <section class="activity-connectors-overview" aria-label="连接器概览">
      <div>
        <strong>{{ connectors.length }}</strong
        ><span>已配置</span>
      </div>
      <div>
        <strong>{{ configuredCount }}</strong
        ><span>正在使用</span>
      </div>
      <div>
        <strong>{{ providers.length }}</strong
        ><span>可选服务</span>
      </div>
    </section>

    <div v-if="loading" class="page-loading activity-connectors-loading"><span class="page-loading-spinner" /></div>
    <section v-else-if="loadError" class="activity-connectors-state" role="alert">
      <NotificationOutlined aria-hidden="true" />
      <strong>活动连接器暂时无法加载</strong>
      <p>{{ loadError }}</p>
      <XButton @click="load"><ReloadOutlined aria-hidden="true" />重试</XButton>
    </section>
    <section v-else-if="connectors.length === 0" class="activity-connectors-state">
      <NotificationOutlined aria-hidden="true" />
      <strong>还没有活动连接器</strong>
      <p>配置后，每条新活动会在本地持久化，再由对应服务可靠转发。</p>
      <XButton variant="primary" @click="beginCreate"><PlusOutlined aria-hidden="true" />添加第一个连接器</XButton>
    </section>
    <div v-else class="activity-connectors-grid">
      <article v-for="connector in connectors" :key="connector.id" class="activity-connector-card">
        <header>
          <span class="activity-connector-icon"><ApiOutlined aria-hidden="true" /></span>
          <div>
            <div class="activity-connector-name-line">
              <h2>{{ connector.name }}</h2>
              <span :class="['activity-connector-status', { disabled: !connector.enabled }]">
                {{ connector.enabled ? '启用' : '停用' }}
              </span>
            </div>
            <p>{{ connector.provider_name }} · {{ connector.provider_id }}</p>
          </div>
        </header>

        <div class="activity-connector-metrics">
          <span
            ><strong>{{ connector.delivery.sent }}</strong
            >已发送</span
          >
          <span
            ><strong>{{ connector.delivery.pending }}</strong
            >待发送</span
          >
          <span :class="{ failed: connector.delivery.failed > 0 }"
            ><strong>{{ connector.delivery.failed }}</strong
            >失败</span
          >
        </div>
        <div class="activity-connector-last">最近发送：{{ formatTime(connector.delivery.last_sent_at) }}</div>
        <p v-if="connector.delivery.last_error" class="activity-connector-error" role="status">
          {{ connector.delivery.last_error }}
        </p>

        <footer>
          <XButton size="small" :disabled="!!testingID" @click="testSaved(connector)">
            <LoadingOutlined v-if="testingID === connector.id" class="activity-connector-spin" aria-hidden="true" />
            <SendOutlined v-else aria-hidden="true" />测试
          </XButton>
          <XButton
            v-if="connector.delivery.failed"
            size="small"
            :disabled="!!retryingID"
            @click="retryFailed(connector)"
          >
            <ReloadOutlined
              :class="{ 'activity-connector-spin': retryingID === connector.id }"
              aria-hidden="true"
            />重试
          </XButton>
          <XButton size="small" @click="beginEdit(connector)"><EditOutlined aria-hidden="true" />编辑</XButton>
          <XButton size="small" variant="danger" @click="requestDelete(connector)">
            <DeleteOutlined aria-hidden="true" />删除
          </XButton>
        </footer>
      </article>
    </div>
  </section>

  <Dialog.Root
    :open="editorOpen"
    lazy-mount
    unmount-on-exit
    :close-on-escape="!saving && !testing"
    :close-on-interact-outside="!saving && !testing"
    @exit-complete="completeEditorClose"
    @update:open="editorDialog.updateOpen($event, !saving && !testing)"
  >
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner activity-connector-editor-positioner">
        <Dialog.Content class="dialog-panel activity-connector-editor">
          <div class="dialog-header">
            <div>
              <Dialog.Title>{{ editing ? '编辑连接器' : '新建连接器' }}</Dialog.Title>
              <Dialog.Description>
                {{
                  selectedProvider
                    ? '填写连接信息并发送测试，确认服务可以正常接收活动。'
                    : '按用途查找要接收活动的服务。'
                }}
              </Dialog.Description>
            </div>
            <button
              class="dialog-close"
              type="button"
              :disabled="saving || testing"
              aria-label="关闭"
              @click="closeEditor"
            >
              <CloseOutlined aria-hidden="true" />
            </button>
          </div>
          <div class="dialog-body activity-connector-editor-body">
            <section v-if="!editing && !selectedProvider" class="activity-connector-provider-directory">
              <Field.Root class="activity-connector-provider-search">
                <Field.Label class="activity-connector-visually-hidden">搜索连接器服务</Field.Label>
                <div class="activity-connector-provider-search-control">
                  <SearchOutlined aria-hidden="true" />
                  <Field.Input v-model="providerSearch" autocomplete="off" placeholder="搜索服务名称、用途或关键词" />
                  <span v-if="providerSearch.trim()">{{ visibleProviderCount }} 个结果</span>
                </div>
              </Field.Root>

              <nav class="activity-connector-provider-categories" aria-label="服务类别">
                <XButton
                  size="small"
                  :variant="activeProviderCategory === '全部' ? 'primary' : 'secondary'"
                  :aria-pressed="activeProviderCategory === '全部'"
                  @click="selectProviderCategory('全部')"
                >
                  全部<span>{{ providers.length }}</span>
                </XButton>
                <XButton
                  v-for="category in providerCategories"
                  :key="category"
                  size="small"
                  :variant="activeProviderCategory === category ? 'primary' : 'secondary'"
                  :aria-pressed="activeProviderCategory === category"
                  @click="selectProviderCategory(category)"
                >
                  {{ category }}<span>{{ providerCategoryCounts.get(category) || 0 }}</span>
                </XButton>
              </nav>

              <div v-if="visibleProviderGroups.length" class="activity-connector-provider-groups">
                <section
                  v-for="group in visibleProviderGroups"
                  :key="group.category"
                  class="activity-connector-provider-group"
                >
                  <header>
                    <h3>{{ group.category }}</h3>
                    <span>{{ group.providers.length }} 个服务</span>
                  </header>
                  <XPrompts
                    class="activity-connector-provider-prompts"
                    :items="
                      group.providers.map((provider) => ({
                        key: provider.id,
                        label: provider.name,
                        description: provider.description,
                        icon: ApiOutlined,
                      }))
                    "
                    @select="selectProviderByPrompt"
                  />
                </section>
              </div>
              <div v-else class="activity-connector-provider-empty">
                <SearchOutlined aria-hidden="true" />
                <strong>没有匹配的服务</strong>
                <span>尝试搜索服务名称、用途或接收渠道。</span>
              </div>
            </section>

            <div v-else-if="selectedProvider" class="activity-connector-selected-provider">
              <span class="activity-connector-provider-icon"><ApiOutlined /></span>
              <div class="activity-connector-selected-provider-copy">
                <div>
                  <strong>{{ selectedProvider.name }}</strong>
                  <span>{{ selectedProvider.category }}</span>
                </div>
                <p>{{ selectedProvider.description }}</p>
              </div>
              <XButton v-if="!editing" size="small" @click="changeProvider">
                <SwapOutlined aria-hidden="true" />更换服务
              </XButton>
            </div>

            <template v-if="selectedProvider">
              <Field.Root class="activity-connector-field">
                <Field.Label>连接器名称</Field.Label>
                <Field.Input v-model="form.name" maxlength="100" autocomplete="off" placeholder="便于识别的名称" />
              </Field.Root>

              <div class="activity-connector-fields">
                <template v-for="field in selectedProvider.fields" :key="field.key">
                  <Checkbox.Root
                    v-if="field.type === 'boolean'"
                    class="activity-connector-checkbox"
                    :checked="form.config[field.key] === true"
                    @update:checked="updateBoolean(field, $event)"
                  >
                    <Checkbox.HiddenInput />
                    <Checkbox.Control
                      ><Checkbox.Indicator><CheckOutlined /></Checkbox.Indicator
                    ></Checkbox.Control>
                    <Checkbox.Label>
                      <strong>{{ field.label }}</strong>
                      <small v-if="field.help">{{ field.help }}</small>
                    </Checkbox.Label>
                  </Checkbox.Root>

                  <Select.Root
                    v-else-if="field.type === 'select'"
                    class="activity-connector-field"
                    :collection="fieldCollection(field)"
                    :model-value="form.config[field.key] === undefined ? [] : [String(form.config[field.key])]"
                    :positioning="{ placement: 'bottom-start', sameWidth: true }"
                    @value-change="updateSelect(field, $event.value)"
                  >
                    <Select.HiddenSelect />
                    <Select.Label>{{ field.label }}<span v-if="field.required"> *</span></Select.Label>
                    <Select.Control>
                      <Select.Trigger class="activity-connector-select-trigger">
                        <Select.ValueText placeholder="请选择" />
                        <Select.Indicator><DownOutlined /></Select.Indicator>
                      </Select.Trigger>
                    </Select.Control>
                    <Teleport to="body">
                      <Select.Positioner class="activity-connector-select-positioner">
                        <Select.Content class="activity-connector-select-content">
                          <Select.Item
                            v-for="option in (field.options || []) as ConnectorFieldOption[]"
                            :key="String(option.value)"
                            :item="{ label: option.label, value: String(option.value) }"
                            class="activity-connector-select-item"
                          >
                            <Select.ItemText>{{ option.label }}</Select.ItemText>
                            <Select.ItemIndicator><CheckOutlined /></Select.ItemIndicator>
                          </Select.Item>
                        </Select.Content>
                      </Select.Positioner>
                    </Teleport>
                  </Select.Root>

                  <Field.Root v-else class="activity-connector-field">
                    <div class="activity-connector-field-label-line">
                      <Field.Label>{{ field.label }}<span v-if="field.required"> *</span></Field.Label>
                      <button
                        v-if="fieldSensitive(field) && configuredSecret(field)"
                        type="button"
                        class="activity-connector-secret-action"
                        @click="toggleSecret(field)"
                      >
                        {{ form.clearSecrets.includes(field.key) ? '保留已保存凭据' : '移除已保存凭据' }}
                      </button>
                    </div>
                    <Field.Textarea
                      v-if="field.type === 'textarea'"
                      :model-value="String(form.config[field.key] ?? '')"
                      :placeholder="field.placeholder"
                      maxlength="65536"
                      @update:model-value="updateField(field, String($event))"
                    />
                    <Field.Input
                      v-else
                      :model-value="form.config[field.key] as string | number"
                      :type="fieldSensitive(field) ? 'password' : field.type === 'number' ? 'number' : 'text'"
                      :autocomplete="fieldSensitive(field) ? 'new-password' : 'off'"
                      :placeholder="
                        fieldSensitive(field) && secretIsPreserved(field) ? '已保存；留空保持不变' : field.placeholder
                      "
                      @update:model-value="updateField(field, String($event))"
                    />
                    <Field.HelperText v-if="field.help">{{ field.help }}</Field.HelperText>
                    <Field.HelperText v-else-if="fieldSensitive(field) && secretIsPreserved(field)">
                      浏览器不会读取已保存的原值。
                    </Field.HelperText>
                  </Field.Root>
                </template>
              </div>

              <Checkbox.Root
                class="activity-connector-checkbox activity-connector-enabled"
                :checked="form.enabled"
                @update:checked="form.enabled = $event === true"
              >
                <Checkbox.HiddenInput />
                <Checkbox.Control
                  ><Checkbox.Indicator><CheckOutlined /></Checkbox.Indicator
                ></Checkbox.Control>
                <Checkbox.Label>
                  <strong>启用连接器</strong>
                  <small>启用后，新发布的活动会自动加入该连接器的投递队列。</small>
                </Checkbox.Label>
              </Checkbox.Root>
            </template>

            <p v-if="editorError" class="activity-connector-dialog-error" role="alert">{{ editorError }}</p>
          </div>
          <div class="dialog-footer activity-connector-editor-actions">
            <XButton :disabled="saving || testing" @click="closeEditor"><CloseOutlined />取消</XButton>
            <XButton v-if="selectedProvider" :disabled="!formValid || saving || testing" @click="testDraft">
              <LoadingOutlined v-if="testing" class="activity-connector-spin" />
              <SendOutlined v-else />{{ testing ? '测试中…' : '发送测试' }}
            </XButton>
            <XButton
              v-if="selectedProvider"
              variant="primary"
              :disabled="!formValid || saving || testing"
              @click="save"
            >
              <LoadingOutlined v-if="saving" class="activity-connector-spin" />
              <SaveOutlined v-else />{{ saving ? '保存中…' : '保存连接器' }}
            </XButton>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>

  <Dialog.Root
    :open="deleteOpen"
    lazy-mount
    unmount-on-exit
    :close-on-escape="!deleting"
    :close-on-interact-outside="!deleting"
    @exit-complete="deleteDialog.clearAfterExit"
    @update:open="deleteDialog.updateOpen($event, !deleting)"
  >
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner">
        <Dialog.Content class="dialog-panel activity-connector-delete-dialog">
          <div class="dialog-header">
            <div>
              <Dialog.Title>删除连接器</Dialog.Title>
              <Dialog.Description>之后发布的新活动将不再通过这个连接器发送。</Dialog.Description>
            </div>
            <button class="dialog-close" :disabled="deleting" aria-label="关闭" @click="deleteDialog.close">
              <CloseOutlined />
            </button>
          </div>
          <div class="dialog-body">
            <p class="activity-connector-delete-copy">确定删除「{{ deleteTarget?.name }}」吗？已有活动不会被删除。</p>
            <p v-if="deleteError" class="activity-connector-dialog-error" role="alert">{{ deleteError }}</p>
            <div class="dialog-footer">
              <XButton :disabled="deleting" @click="deleteDialog.close"><CloseOutlined />取消</XButton>
              <XButton variant="danger" :disabled="deleting" @click="confirmDelete">
                <DeleteOutlined />{{ deleting ? '删除中…' : '确认删除' }}
              </XButton>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>

  <Toaster v-slot="toast" :toaster="toaster">
    <Toast.Root class="activity-connectors-toast" :data-type="toast.type">
      <CheckCircleOutlined v-if="toast.type === 'success'" aria-hidden="true" />
      <CloseOutlined v-else aria-hidden="true" />
      <Toast.Title>{{ toast.title }}</Toast.Title>
    </Toast.Root>
  </Toaster>
</template>

<style lang="scss">
.activity-connectors-page {
  width: 100%;
  color: var(--text-primary);
}

.activity-connectors-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 20px;
  h1 {
    margin: 0 0 5px;
    font-size: var(--marvo-type-20);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--marvo-type-13);
  }
}

.activity-connectors-overview {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 18px;
  > div {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 16px 18px;
    border: 1px solid var(--border-primary);
    border-radius: 12px;
    background: var(--bg-primary);
  }
  strong {
    font-size: var(--marvo-type-22);
  }
  span {
    color: var(--text-tertiary);
    font-size: var(--marvo-type-12);
  }
}

.activity-connectors-loading {
  min-height: 220px;
}
.activity-connectors-state {
  min-height: 260px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 32px;
  text-align: center;
  border: 1px dashed var(--border-primary);
  border-radius: 14px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  > .anticon {
    font-size: var(--marvo-type-28);
    color: var(--text-accent);
  }
  strong {
    color: var(--text-primary);
    font-size: var(--marvo-type-16);
  }
  p {
    max-width: 520px;
    margin: 0 0 6px;
    font-size: var(--marvo-type-13);
    line-height: 1.6;
  }
}

.activity-connectors-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 360px), 1fr));
  gap: 14px;
}
.activity-connector-card {
  min-width: 0;
  padding: 18px;
  border: 1px solid var(--border-primary);
  border-radius: 14px;
  background: var(--bg-primary);
  > header {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 11px;
    align-items: center;
  }
  h2 {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
    font-size: var(--marvo-type-15);
  }
  header p {
    margin: 3px 0 0;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-11);
  }
  > footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 7px;
    margin-top: 16px;
  }
}
.activity-connector-icon,
.activity-connector-provider-icon {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  flex: none;
  border-radius: 9px;
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 10%, transparent);
  color: var(--text-accent);
}
.activity-connector-name-line {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.activity-connector-status {
  flex: none;
  padding: 2px 7px;
  border-radius: 999px;
  color: var(--text-accent);
  background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 10%, transparent);
  font-size: var(--marvo-type-10);
  white-space: nowrap;
  &.disabled {
    background: var(--bg-secondary);
    color: var(--text-muted);
  }
}
.activity-connector-metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  margin-top: 17px;
  span {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px;
    border-radius: 9px;
    background: var(--bg-secondary);
    color: var(--text-tertiary);
    font-size: var(--marvo-type-10);
  }
  strong {
    color: var(--text-primary);
    font-size: var(--marvo-type-15);
  }
  .failed strong {
    color: var(--text-danger);
  }
}
.activity-connector-last {
  margin-top: 11px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.activity-connector-error {
  margin: 10px 0 0;
  padding: 9px 10px;
  border-radius: 8px;
  overflow-wrap: anywhere;
  background: color-mix(in srgb, var(--text-danger) 8%, transparent);
  color: var(--text-danger);
  font-size: var(--marvo-type-11);
}

.activity-connector-editor {
  max-width: 900px;
  z-index: calc(1000 + var(--layer-index, 0));
}
.activity-connector-editor-body {
  min-height: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.activity-connector-field [data-part='label'] {
  display: block;
  margin-bottom: 7px;
  color: var(--text-primary);
  font-size: var(--marvo-type-12);
  font-weight: 600;
}
.activity-connector-select-trigger,
.activity-connector-field :is([data-part='input'], [data-part='textarea']) {
  width: 100%;
  min-height: 40px;
  box-sizing: border-box;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  outline: none;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
  &:focus,
  &:focus-within {
    border-color: var(--text-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 12%, transparent);
  }
}
.activity-connector-select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  cursor: pointer;
}
.activity-connector-field [data-part='input'] {
  padding: 0 12px;
}
.activity-connector-field [data-part='textarea'] {
  min-height: 110px;
  padding: 10px 12px;
  resize: vertical;
  line-height: 1.55;
}
.activity-connector-field [data-part='helper-text'] {
  display: block;
  margin-top: 5px;
  color: var(--text-tertiary);
  font-size: var(--marvo-type-11);
}
.activity-connector-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.activity-connector-field:has([data-part='textarea']) {
  grid-column: 1 / -1;
}
.activity-connector-field-label-line {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}
.activity-connector-secret-action {
  border: 0;
  background: transparent;
  color: var(--text-accent);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-10);
}
.activity-connector-provider-directory {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.activity-connector-visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.activity-connector-provider-search-control {
  min-height: 42px;
  display: flex;
  align-items: center;
  gap: 9px;
  padding-inline: 12px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-primary);
  color: var(--text-tertiary);
  transition:
    border-color 0.16s,
    box-shadow 0.16s;
  &:focus-within {
    border-color: var(--text-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 12%, transparent);
  }
  > .anticon {
    flex: none;
    font-size: var(--marvo-type-15);
  }
  [data-part='input'] {
    min-width: 0;
    flex: 1;
    align-self: stretch;
    padding: 0;
    border: 0;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--marvo-type-13);
  }
  > span {
    flex: none;
    font-size: var(--marvo-type-11);
    white-space: nowrap;
  }
}
.activity-connector-provider-categories {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  .x-button {
    border-radius: 999px;
  }
  .x-button > span {
    min-width: 18px;
    padding: 1px 5px;
    border-radius: 999px;
    background: color-mix(in srgb, currentColor 10%, transparent);
    font-size: var(--marvo-type-10);
    line-height: 1.35;
  }
}
.activity-connector-provider-groups {
  display: flex;
  flex-direction: column;
  gap: 22px;
}
.activity-connector-provider-group {
  min-width: 0;
  > header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 9px;
  }
  h3 {
    margin: 0;
    color: var(--text-primary);
    font-size: var(--marvo-type-13);
    font-weight: 600;
  }
  > header span {
    color: var(--text-muted);
    font-size: var(--marvo-type-11);
  }
}
.activity-connector-provider-prompts {
  .x-prompts-list {
    gap: 9px;
  }
  .x-prompts-item {
    flex: 1 1 calc(50% - 5px);
    min-height: 76px;
    background: var(--bg-primary);
  }
  .x-prompts-content small {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }
}
.activity-connector-provider-empty {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 24px;
  border: 1px dashed var(--border-primary);
  border-radius: 12px;
  text-align: center;
  color: var(--text-tertiary);
  > .anticon {
    margin-bottom: 3px;
    font-size: var(--marvo-type-22);
  }
  strong {
    color: var(--text-primary);
    font-size: var(--marvo-type-13);
  }
  span {
    font-size: var(--marvo-type-12);
  }
}
.activity-connector-selected-provider {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 13px;
  border-radius: 10px;
  background: var(--bg-secondary);
}
.activity-connector-selected-provider-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  > div {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  strong {
    font-size: var(--marvo-type-13);
  }
  > div span {
    padding: 2px 7px;
    border-radius: 999px;
    background: var(--bg-primary);
    color: var(--text-tertiary);
    font-size: var(--marvo-type-10);
    white-space: nowrap;
  }
  p {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--marvo-type-11);
    line-height: 1.45;
  }
}
.activity-connector-checkbox {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  cursor: pointer;
  [data-part='control'] {
    width: 18px;
    height: 18px;
    display: grid;
    place-items: center;
    flex: none;
    margin-top: 1px;
    border: 1px solid var(--border-primary);
    border-radius: 5px;
    background: var(--bg-primary);
    color: #fff;
  }
  [data-part='control'][data-state='checked'] {
    border-color: var(--marvo-accent-color, #4f46e5);
    background: var(--marvo-accent-color, #4f46e5);
  }
  [data-part='indicator']:not([hidden]) {
    display: grid;
    font-size: 11px;
  }
  [data-part='indicator'][hidden] {
    display: none;
  }
  [data-part='label'] {
    display: flex;
    flex-direction: column;
    gap: 2px;
    color: var(--text-primary);
    font-size: var(--marvo-type-12);
  }
  small {
    color: var(--text-tertiary);
    font-weight: 400;
    line-height: 1.45;
  }
}
.activity-connector-enabled {
  padding: 13px;
  border: 1px solid var(--border-light);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.activity-connector-dialog-error {
  margin: 0;
  color: var(--text-danger);
  font-size: var(--marvo-type-12);
  overflow-wrap: anywhere;
}
.activity-connector-editor-actions {
  flex: none;
  margin-top: 0;
  padding: 14px 24px 18px;
  border-top: 1px solid var(--border-light);
  background: var(--bg-card);
}
.activity-connector-delete-dialog {
  max-width: 430px;
}
.activity-connector-delete-copy {
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
  line-height: 1.6;
}
.activity-connector-spin {
  animation: activity-connector-spin 0.8s linear infinite;
}
@keyframes activity-connector-spin {
  to {
    transform: rotate(360deg);
  }
}

.activity-connector-select-positioner {
  z-index: var(--z-index, 1000);
}
.activity-connector-select-content {
  z-index: calc(1000 + var(--layer-index, 0));
  max-height: min(360px, var(--available-height));
  overflow-y: auto;
  padding: 6px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
  outline: none;
}
.activity-connector-select-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 7px;
  cursor: pointer;
  outline: none;
  font-size: var(--marvo-type-12);
  &[data-highlighted] {
    background: var(--bg-hover);
  }
  [data-part='item-indicator'] {
    color: var(--text-accent);
  }
}

.activity-connectors-toast {
  min-width: 220px;
  max-width: min(420px, calc(100vw - 28px));
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 11px 14px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-card);
  color: var(--text-primary);
  box-shadow: var(--shadow-card);
  font-size: var(--marvo-type-12);
  > .anticon {
    color: var(--text-accent);
  }
  &[data-type='error'] > .anticon {
    color: var(--text-danger);
  }
}

@media (max-width: 700px) {
  .activity-connectors-heading {
    align-items: stretch;
    flex-direction: column;
  }
  .activity-connectors-heading .x-button {
    align-self: flex-start;
  }
  .activity-connectors-overview {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .activity-connectors-overview > div {
    align-items: flex-start;
    flex-direction: column;
    gap: 4px;
    padding: 12px;
  }
  .activity-connector-fields {
    grid-template-columns: 1fr;
  }
  .activity-connector-provider-prompts .x-prompts-item {
    flex-basis: 100%;
  }
  .activity-connector-provider-categories {
    flex-wrap: nowrap;
    margin-inline: -14px;
    padding-inline: 14px;
    overflow-x: auto;
    scrollbar-width: none;
    .x-button {
      flex: none;
    }
    &::-webkit-scrollbar {
      display: none;
    }
  }
  .activity-connector-selected-provider {
    grid-template-columns: auto minmax(0, 1fr);
    .x-button {
      grid-column: 1 / -1;
      justify-self: end;
    }
  }
  .activity-connector-field:has([data-part='textarea']) {
    grid-column: auto;
  }
  .activity-connector-editor-positioner {
    padding: 10px;
    align-items: flex-end;
  }
  .activity-connector-editor {
    height: calc(100% - 20px);
    max-height: calc(100% - 20px);
    border-radius: 14px;
  }
  .activity-connector-editor-actions {
    padding: 12px 14px 14px;
  }
}
</style>
