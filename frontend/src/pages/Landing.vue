<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowRightOutlined,
  CheckCircleFilled,
  FileTextOutlined,
  GithubOutlined,
  LinkOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SearchOutlined,
  SyncOutlined,
} from '@ant-design/icons-vue'
import MarvoMark from '../components/MarvoMark.vue'

const router = useRouter()
const spaceEntry = ref('')
const spaceEntryError = ref('')
const spaceInput = ref<HTMLInputElement>()
const spaceIDPattern = /^[0-9a-f]{20}$/i
const spacePathPattern = /(?:^|\/)user\/([0-9a-f]{20})(?:\/|$)/i

const normalizedSpaceID = computed(() => {
  const value = spaceEntry.value.trim()
  if (spaceIDPattern.test(value)) return value.toLowerCase()

  try {
    const url = new URL(value, window.location.origin)
    return url.pathname.match(spacePathPattern)?.[1]?.toLowerCase() || ''
  } catch {
    return ''
  }
})

function openSpace() {
  if (!normalizedSpaceID.value) {
    spaceEntryError.value = '请输入有效的空间链接或 20 位空间 ID'
    return
  }
  spaceEntryError.value = ''
  void router.push(`/user/${normalizedSpaceID.value}`)
}

async function focusSpaceEntry() {
  document.querySelector('#space-entry')?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  await nextTick()
  spaceInput.value?.focus({ preventScroll: true })
}

function clearSpaceError() {
  if (spaceEntryError.value) spaceEntryError.value = ''
}
</script>

<template>
  <main class="landing-page">
    <header class="landing-header">
      <router-link class="landing-brand" to="/" aria-label="Marvo 首页">
        <MarvoMark />
        <span>Marvo</span>
      </router-link>

      <nav class="landing-navigation" aria-label="主要导航">
        <a class="landing-nav-link" href="https://github.com/willvar/marvo" target="_blank" rel="noreferrer">
          <GithubOutlined aria-hidden="true" />
          <span>开源项目</span>
        </a>
        <router-link class="landing-admin-link" to="/admin">
          <SafetyCertificateOutlined aria-hidden="true" />
          <span>平台管理</span>
        </router-link>
      </nav>
    </header>

    <section class="landing-hero" aria-labelledby="landing-title">
      <div class="landing-hero-copy">
        <div class="landing-eyebrow">
          <span class="landing-eyebrow-mark"><MarvoMark /></span>
          <span>AI 原生知识空间</span>
        </div>
        <h1 id="landing-title"><span>不只保存知识，</span><span>更让它参与工作。</span></h1>
        <p class="landing-lead">
          Marvo
          将笔记、资料与智能体汇聚在一个持续生长的空间中。你决定方向，智能体理解已有内容、开展研究、分析与创作，再将成果沉淀其中。
        </p>
        <div class="landing-hero-actions">
          <button type="button" class="landing-primary-action" @click="focusSpaceEntry">
            <LinkOutlined aria-hidden="true" />
            <span>进入我的空间</span>
            <ArrowRightOutlined aria-hidden="true" />
          </button>
          <a class="landing-secondary-action" href="#capabilities">
            <span>看看它如何工作</span>
            <ArrowRightOutlined aria-hidden="true" />
          </a>
        </div>
        <div class="landing-trust-row" aria-label="产品特点">
          <span><CheckCircleFilled aria-hidden="true" />知识持续积累</span>
          <span><CheckCircleFilled aria-hidden="true" />智能体参与工作</span>
          <span><CheckCircleFilled aria-hidden="true" />数据由你掌握</span>
        </div>
      </div>

      <div class="landing-product-frame" aria-label="Marvo 工作区界面示意">
        <div class="landing-product-glow" />
        <div class="landing-product-window">
          <div class="landing-window-bar">
            <div class="landing-window-dots" aria-hidden="true"><span /><span /><span /></div>
            <div class="landing-window-title"><MarvoMark /><span>研究空间</span></div>
            <div class="landing-window-state"><span />已保存</div>
          </div>
          <div class="landing-workspace-preview">
            <aside class="landing-preview-sidebar">
              <div class="landing-preview-brand"><MarvoMark /><strong>码窝</strong></div>
              <div class="landing-preview-search"><SearchOutlined aria-hidden="true" /><span>搜索或新建</span></div>
              <div class="landing-preview-nav-title">最近笔记</div>
              <div class="landing-preview-note active">
                <FileTextOutlined aria-hidden="true" /><span>智能体时代的知识工作</span>
              </div>
              <div class="landing-preview-note"><FileTextOutlined aria-hidden="true" /><span>研究资料与引用</span></div>
              <div class="landing-preview-note"><FileTextOutlined aria-hidden="true" /><span>产品构想</span></div>
            </aside>

            <article class="landing-preview-document">
              <div class="landing-document-meta"><span>研究</span><span>知识管理</span><span>智能体</span></div>
              <h2>智能体时代的知识工作</h2>
              <p>这次研究从既有笔记和一组新资料开始。智能体将分散的观点重新联系起来，逐步整理成可以继续使用的成果。</p>
              <h3>从记录到行动</h3>
              <div class="landing-document-callout">
                <span class="landing-callout-icon"><RobotOutlined aria-hidden="true" /></span>
                <div>
                  <strong>智能体正在补充研究结论</strong>
                  <p>已关联 6 篇笔记和 8 条资料，结论将直接写入当前内容。</p>
                </div>
              </div>
              <div class="landing-document-lines" aria-hidden="true"><span /><span /><span /><span /></div>
            </article>

            <aside class="landing-preview-agent">
              <div class="landing-agent-heading">
                <span class="landing-agent-icon"><RobotOutlined aria-hidden="true" /></span>
                <div><strong>智能体</strong><span>理解整个知识空间</span></div>
              </div>
              <div class="landing-agent-request">结合空间中已有的研究，分析这些资料的共同趋势。</div>
              <div class="landing-agent-progress">
                <span class="landing-agent-step done"><CheckCircleFilled aria-hidden="true" /></span>
                <div><strong>理解空间上下文</strong><span>已关联 6 篇相关笔记</span></div>
              </div>
              <div class="landing-agent-progress">
                <span class="landing-agent-step active"><SearchOutlined aria-hidden="true" /></span>
                <div><strong>研究新增资料</strong><span>正在交叉验证关键观点</span></div>
              </div>
              <div class="landing-agent-answer">我已将共同趋势、主要分歧和待验证的问题整理到当前笔记。</div>
              <div class="landing-agent-composer">
                <span>继续研究或修改内容…</span><ArrowRightOutlined aria-hidden="true" />
              </div>
            </aside>
          </div>
        </div>
      </div>
    </section>

    <section id="capabilities" class="landing-capabilities" aria-labelledby="capabilities-title">
      <div class="landing-section-heading">
        <p>让知识形成闭环</p>
        <h2 id="capabilities-title">每一次工作，都成为下一次的基础</h2>
      </div>
      <div class="landing-capability-grid">
        <article>
          <span class="landing-capability-icon"><FileTextOutlined aria-hidden="true" /></span>
          <h3>把正在想的留在这里</h3>
          <p>随手记录想法，整理资料和媒体，不必先为每一条内容设计复杂的结构。</p>
        </article>
        <article>
          <span class="landing-capability-icon"><RobotOutlined aria-hidden="true" /></span>
          <h3>让智能体理解上下文</h3>
          <p>它从当前内容与已有知识出发，理解你正在做什么，不必每次都从零解释背景。</p>
        </article>
        <article>
          <span class="landing-capability-icon"><SearchOutlined aria-hidden="true" /></span>
          <h3>推进研究与创作</h3>
          <p>检索信息、比较材料、分析关系并参与创作。过程保持可见，结果直接进入当前内容。</p>
        </article>
        <article>
          <span class="landing-capability-icon"><SyncOutlined aria-hidden="true" /></span>
          <h3>让成果回到知识中</h3>
          <p>有价值的结论不只留在对话里。它们被整理进空间，成为后续思考与工作的基础。</p>
        </article>
      </div>
    </section>

    <section id="space-entry" class="landing-space-entry" aria-labelledby="space-entry-title">
      <div class="landing-space-entry-copy">
        <span class="landing-space-entry-icon"><LinkOutlined aria-hidden="true" /></span>
        <div>
          <h2 id="space-entry-title">进入你的空间</h2>
          <p>粘贴空间管理员分享的专属链接，或在这里输入空间 ID。</p>
        </div>
      </div>
      <form class="landing-space-form" @submit.prevent="openSpace">
        <div class="landing-space-control">
          <input
            ref="spaceInput"
            v-model="spaceEntry"
            type="text"
            inputmode="text"
            autocomplete="off"
            spellcheck="false"
            aria-label="用户空间链接或空间 ID"
            :aria-invalid="!!spaceEntryError"
            :aria-describedby="spaceEntryError ? 'space-entry-error' : undefined"
            placeholder="粘贴空间链接或输入空间 ID"
            @input="clearSpaceError"
          />
          <button type="submit">
            <span>打开空间</span>
            <ArrowRightOutlined aria-hidden="true" />
          </button>
        </div>
        <p v-if="spaceEntryError" id="space-entry-error" class="landing-space-error" role="alert">
          {{ spaceEntryError }}
        </p>
      </form>
    </section>

    <footer class="landing-footer">
      <router-link class="landing-footer-brand" to="/"><MarvoMark /><span>Marvo</span></router-link>
      <p>AI 原生知识空间</p>
      <div>
        <a href="https://github.com/willvar/marvo" target="_blank" rel="noreferrer">GitHub</a>
        <router-link to="/admin">平台管理</router-link>
      </div>
    </footer>
  </main>
</template>

<style scoped lang="scss">
.landing-page {
  --landing-accent: #5848f5;
  --landing-accent-soft: color-mix(in srgb, var(--landing-accent) 11%, var(--bg-primary));
  --landing-border: color-mix(in srgb, var(--border-primary) 82%, transparent);
  height: 100%;
  overflow-x: hidden;
  overflow-y: auto;
  background:
    radial-gradient(circle at 76% 8%, color-mix(in srgb, var(--landing-accent) 13%, transparent), transparent 27rem),
    radial-gradient(circle at 8% 42%, color-mix(in srgb, #0ea5e9 7%, transparent), transparent 24rem), var(--bg-primary);
  color: var(--text-primary);
  scroll-behavior: smooth;
}

.landing-header {
  width: min(1380px, calc(100% - 64px));
  min-height: 76px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
}

.landing-brand,
.landing-footer-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--text-primary);
  text-decoration: none;
  font-size: 1.22rem;
  font-weight: 750;
  letter-spacing: -0.02em;

  .marvo-mark {
    width: 30px;
    height: 30px;
    color: var(--landing-accent);
  }
}

.landing-navigation,
.landing-nav-link,
.landing-admin-link,
.landing-hero-actions,
.landing-primary-action,
.landing-secondary-action {
  display: flex;
  align-items: center;
}

.landing-navigation {
  gap: 8px;
}

.landing-nav-link,
.landing-admin-link {
  justify-content: center;
  gap: 7px;
  min-height: 40px;
  padding: 0 14px;
  border-radius: 10px;
  color: var(--text-secondary);
  text-decoration: none;
  font-weight: 600;
  transition:
    color 0.16s,
    background 0.16s,
    border-color 0.16s;

  &:hover {
    color: var(--text-primary);
    background: var(--bg-hover);
  }
}

.landing-admin-link {
  border: 1px solid var(--landing-border);
  background: color-mix(in srgb, var(--bg-primary) 88%, transparent);
}

.landing-hero {
  width: min(1380px, calc(100% - 64px));
  min-height: min(760px, calc(100vh - 76px));
  margin: 0 auto;
  padding: 72px 0 104px;
  display: grid;
  grid-template-columns: minmax(390px, 0.78fr) minmax(620px, 1.22fr);
  align-items: center;
  gap: clamp(48px, 6vw, 108px);
}

.landing-hero-copy {
  position: relative;
  z-index: 2;
}

.landing-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 22px;
  color: var(--text-secondary);
  font-size: 0.82rem;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.landing-eyebrow-mark {
  width: 25px;
  height: 25px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  background: var(--landing-accent-soft);
  color: var(--landing-accent);

  .marvo-mark {
    width: 15px;
    height: 15px;
  }
}

.landing-hero h1 {
  max-width: 670px;
  font-size: clamp(2.5rem, 4vw, 4.35rem);
  line-height: 1.08;
  letter-spacing: -0.055em;
  font-weight: 780;
  text-wrap: balance;

  span {
    display: block;
    white-space: nowrap;
  }
}

.landing-lead {
  max-width: 620px;
  margin-top: 28px;
  color: var(--text-tertiary);
  font-size: clamp(1.08rem, 1.25vw, 1.3rem);
  line-height: 1.75;
}

.landing-hero-actions {
  gap: 12px;
  margin-top: 34px;
}

.landing-primary-action,
.landing-secondary-action {
  min-height: 48px;
  justify-content: center;
  gap: 9px;
  padding: 0 19px;
  border-radius: 12px;
  font: inherit;
  font-weight: 650;
  cursor: pointer;
  text-decoration: none;
  white-space: nowrap;
  transition:
    transform 0.16s,
    box-shadow 0.16s,
    background 0.16s,
    color 0.16s;
}

.landing-primary-action {
  border: 1px solid var(--landing-accent);
  background: var(--landing-accent);
  color: #fff;
  box-shadow: 0 12px 28px color-mix(in srgb, var(--landing-accent) 25%, transparent);

  &:hover {
    transform: translateY(-1px);
    box-shadow: 0 16px 34px color-mix(in srgb, var(--landing-accent) 32%, transparent);
  }
}

.landing-secondary-action {
  border: 1px solid var(--landing-border);
  background: color-mix(in srgb, var(--bg-primary) 80%, transparent);
  color: var(--text-secondary);

  &:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
}

.landing-trust-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 20px;
  margin-top: 28px;
  color: var(--text-tertiary);
  font-size: 0.86rem;

  span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  svg {
    color: color-mix(in srgb, var(--landing-accent) 78%, var(--text-primary));
  }
}

.landing-product-frame {
  position: relative;
  min-width: 0;
  perspective: 1200px;
}

.landing-product-glow {
  position: absolute;
  inset: 7% 4% -5% 10%;
  border-radius: 42px;
  background: color-mix(in srgb, var(--landing-accent) 20%, transparent);
  filter: blur(42px);
  opacity: 0.62;
}

.landing-product-window {
  position: relative;
  min-height: 510px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--border-primary) 74%, var(--text-muted));
  border-radius: 18px;
  background: var(--bg-primary);
  box-shadow:
    0 30px 80px rgba(23, 20, 59, 0.16),
    0 8px 24px rgba(23, 20, 59, 0.08);
  transform: rotateY(-1.8deg) rotateX(0.7deg);
  transform-origin: center;
}

.landing-window-bar {
  height: 48px;
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  padding: 0 16px;
  border-bottom: 1px solid var(--border-primary);
  background: color-mix(in srgb, var(--bg-secondary) 85%, var(--bg-primary));
}

.landing-window-dots {
  display: flex;
  gap: 6px;

  span {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--border-light);
  }
}

.landing-window-title {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--text-secondary);
  font-size: 0.76rem;
  font-weight: 650;

  .marvo-mark {
    width: 15px;
    height: 15px;
    color: var(--landing-accent);
  }
}

.landing-window-state {
  justify-self: end;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-muted);
  font-size: 0.67rem;

  span {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #22c55e;
  }
}

.landing-workspace-preview {
  height: 462px;
  display: grid;
  grid-template-columns: 152px minmax(250px, 1fr) 216px;
}

.landing-preview-sidebar {
  min-width: 0;
  padding: 18px 12px;
  border-right: 1px solid var(--border-primary);
  background: var(--bg-secondary);
}

.landing-preview-brand {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 5px 16px;
  font-size: 0.75rem;

  .marvo-mark {
    width: 17px;
    height: 17px;
    color: var(--landing-accent);
  }
}

.landing-preview-search {
  height: 29px;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  border: 1px solid var(--border-primary);
  border-radius: 7px;
  background: var(--bg-primary);
  color: var(--text-muted);
  font-size: 0.63rem;
}

.landing-preview-nav-title {
  padding: 20px 7px 7px;
  color: var(--text-muted);
  font-size: 0.59rem;
  font-weight: 700;
  letter-spacing: 0.06em;
}

.landing-preview-note {
  min-width: 0;
  height: 31px;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 8px;
  border-radius: 7px;
  color: var(--text-tertiary);
  font-size: 0.62rem;

  svg {
    flex: 0 0 auto;
  }

  span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &.active {
    background: var(--landing-accent-soft);
    color: var(--text-accent);
  }
}

.landing-preview-document {
  min-width: 0;
  padding: 47px clamp(24px, 3vw, 45px);
  overflow: hidden;

  h2 {
    margin: 12px 0 18px;
    font-size: clamp(1.25rem, 1.55vw, 1.65rem);
    letter-spacing: -0.035em;
  }

  > p {
    color: var(--text-tertiary);
    font-size: 0.72rem;
    line-height: 1.9;
  }

  h3 {
    margin: 28px 0 12px;
    font-size: 0.8rem;
  }
}

.landing-document-meta {
  display: flex;
  gap: 6px;

  span {
    padding: 3px 7px;
    border-radius: 999px;
    background: var(--bg-tertiary);
    color: var(--text-tertiary);
    font-size: 0.56rem;
  }
}

.landing-document-callout {
  display: flex;
  gap: 10px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--landing-accent) 17%, var(--border-primary));
  border-radius: 10px;
  background: var(--landing-accent-soft);

  strong {
    display: block;
    margin-bottom: 4px;
    color: var(--text-secondary);
    font-size: 0.67rem;
  }

  p {
    color: var(--text-tertiary);
    font-size: 0.59rem;
    line-height: 1.55;
  }
}

.landing-callout-icon,
.landing-agent-icon,
.landing-capability-icon,
.landing-space-entry-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--landing-accent);
}

.landing-callout-icon {
  width: 24px;
  height: 24px;
  flex: 0 0 24px;
  border-radius: 7px;
  background: color-mix(in srgb, var(--landing-accent) 12%, var(--bg-primary));
}

.landing-document-lines {
  display: grid;
  gap: 9px;
  margin-top: 22px;

  span {
    height: 5px;
    border-radius: 4px;
    background: var(--bg-tertiary);

    &:nth-child(2) {
      width: 88%;
    }
    &:nth-child(3) {
      width: 94%;
    }
    &:nth-child(4) {
      width: 64%;
    }
  }
}

.landing-preview-agent {
  min-width: 0;
  padding: 20px 14px;
  border-left: 1px solid var(--border-primary);
  background: color-mix(in srgb, var(--bg-secondary) 70%, var(--bg-primary));
}

.landing-agent-heading {
  display: flex;
  align-items: center;
  gap: 9px;
  padding-bottom: 17px;
  border-bottom: 1px solid var(--border-primary);

  > div {
    display: grid;
    gap: 2px;
  }

  strong {
    font-size: 0.7rem;
  }

  div span {
    color: var(--text-muted);
    font-size: 0.56rem;
  }
}

.landing-agent-icon {
  width: 29px;
  height: 29px;
  border-radius: 9px;
  background: var(--landing-accent-soft);
}

.landing-agent-request {
  width: 91%;
  margin: 19px 0 21px auto;
  padding: 9px 10px;
  border-radius: 9px 9px 3px 9px;
  background: var(--landing-accent);
  color: #fff;
  font-size: 0.58rem;
  line-height: 1.55;
}

.landing-agent-progress {
  display: flex;
  gap: 8px;
  margin: 12px 0;

  > div {
    display: grid;
    gap: 3px;
  }

  strong {
    color: var(--text-secondary);
    font-size: 0.6rem;
  }

  div span {
    color: var(--text-muted);
    font-size: 0.53rem;
  }
}

.landing-agent-step {
  width: 17px;
  height: 17px;
  flex: 0 0 17px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  color: var(--text-muted);
  font-size: 0.58rem;

  &.done {
    color: #22c55e;
  }

  &.active {
    background: var(--landing-accent-soft);
    color: var(--landing-accent);
  }
}

.landing-agent-answer {
  margin-top: 20px;
  padding-top: 17px;
  border-top: 1px solid var(--border-primary);
  color: var(--text-secondary);
  font-size: 0.62rem;
  line-height: 1.75;
}

.landing-agent-composer {
  height: 36px;
  margin-top: 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 10px;
  border: 1px solid var(--border-primary);
  border-radius: 9px;
  background: var(--bg-primary);
  color: var(--text-muted);
  font-size: 0.55rem;

  svg {
    color: var(--landing-accent);
  }
}

.landing-capabilities {
  width: min(1240px, calc(100% - 64px));
  margin: 0 auto;
  padding: 100px 0 110px;
  border-top: 1px solid var(--landing-border);
}

.landing-section-heading {
  max-width: 670px;

  p {
    margin-bottom: 12px;
    color: var(--landing-accent);
    font-size: 0.83rem;
    font-weight: 750;
    letter-spacing: 0.08em;
  }

  h2 {
    font-size: clamp(2rem, 3vw, 3.2rem);
    line-height: 1.2;
    letter-spacing: -0.045em;
  }
}

.landing-capability-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 18px;
  margin-top: 48px;

  article {
    min-height: 260px;
    padding: 28px;
    border: 1px solid var(--landing-border);
    border-radius: 18px;
    background: color-mix(in srgb, var(--bg-card) 92%, transparent);
    transition:
      transform 0.18s,
      border-color 0.18s,
      box-shadow 0.18s;

    &:hover {
      transform: translateY(-3px);
      border-color: color-mix(in srgb, var(--landing-accent) 30%, var(--border-primary));
      box-shadow: 0 18px 38px rgba(23, 20, 59, 0.08);
    }
  }

  h3 {
    margin: 24px 0 12px;
    font-size: 1.12rem;
  }

  p {
    color: var(--text-tertiary);
    font-size: 0.93rem;
    line-height: 1.75;
  }
}

.landing-capability-icon {
  width: 44px;
  height: 44px;
  border-radius: 13px;
  background: var(--landing-accent-soft);
  font-size: 1.25rem;
}

.landing-space-entry {
  width: min(1240px, calc(100% - 64px));
  margin: 0 auto 100px;
  padding: 38px 40px;
  display: grid;
  grid-template-columns: minmax(280px, 0.8fr) minmax(400px, 1.2fr);
  align-items: center;
  gap: 52px;
  border: 1px solid color-mix(in srgb, var(--landing-accent) 18%, var(--border-primary));
  border-radius: 22px;
  background: linear-gradient(120deg, var(--landing-accent-soft), transparent 54%), var(--bg-secondary);
}

.landing-space-entry-copy {
  display: flex;
  align-items: center;
  gap: 17px;

  h2 {
    margin-bottom: 5px;
    font-size: 1.3rem;
  }

  p {
    color: var(--text-tertiary);
    line-height: 1.6;
  }
}

.landing-space-entry-icon {
  width: 48px;
  height: 48px;
  flex: 0 0 48px;
  border-radius: 14px;
  background: var(--bg-primary);
  box-shadow: 0 8px 20px rgba(23, 20, 59, 0.08);
  font-size: 1.2rem;
}

.landing-space-form {
  min-width: 0;
}

.landing-space-control {
  height: 50px;
  display: flex;
  align-items: stretch;
  padding: 4px;
  border: 1px solid var(--border-light);
  border-radius: 12px;
  background: var(--bg-primary);
  box-shadow: 0 8px 24px rgba(23, 20, 59, 0.06);
  transition:
    border-color 0.16s,
    box-shadow 0.16s;

  &:focus-within {
    border-color: var(--landing-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--landing-accent) 13%, transparent);
  }

  input {
    min-width: 0;
    flex: 1;
    padding: 0 12px;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--text-primary);
    font: inherit;

    &::placeholder {
      color: var(--text-muted);
    }
  }

  button {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 0 15px;
    border: 0;
    border-radius: 8px;
    background: var(--landing-accent);
    color: #fff;
    cursor: pointer;
    font: inherit;
    font-weight: 650;
  }
}

.landing-space-error {
  margin: 8px 5px 0;
  color: var(--text-danger);
  font-size: 0.83rem;
}

.landing-footer {
  width: min(1380px, calc(100% - 64px));
  min-height: 96px;
  margin: 0 auto;
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 24px;
  border-top: 1px solid var(--landing-border);
  color: var(--text-muted);
  font-size: 0.82rem;

  > p {
    text-align: center;
  }

  > div {
    justify-self: end;
    display: flex;
    gap: 18px;
  }

  a {
    color: var(--text-tertiary);
    text-decoration: none;

    &:hover {
      color: var(--text-primary);
    }
  }
}

.landing-footer-brand {
  font-size: 0.92rem;

  .marvo-mark {
    width: 23px;
    height: 23px;
  }
}

@media (max-width: 1180px) {
  .landing-hero {
    grid-template-columns: minmax(330px, 0.72fr) minmax(530px, 1.28fr);
    gap: 44px;
  }

  .landing-workspace-preview {
    grid-template-columns: 128px minmax(220px, 1fr) 188px;
  }

  .landing-primary-action,
  .landing-secondary-action {
    gap: 7px;
    padding-inline: 11px;
    font-size: 0.93rem;
  }

  .landing-capability-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));

    article {
      min-height: 220px;
    }
  }
}

@media (max-width: 960px) {
  .landing-hero {
    min-height: 0;
    padding: 68px 0 88px;
    grid-template-columns: 1fr;
  }

  .landing-hero-copy {
    max-width: 720px;
  }

  .landing-hero h1 {
    font-size: clamp(3rem, 7vw, 4.4rem);
  }

  .landing-product-window {
    transform: none;
  }

  .landing-space-entry {
    grid-template-columns: 1fr;
    gap: 25px;
  }
}

@media (max-width: 680px) {
  .landing-header,
  .landing-hero,
  .landing-capabilities,
  .landing-space-entry,
  .landing-footer {
    width: min(100% - 32px, 1380px);
  }

  .landing-header {
    min-height: 64px;
  }

  .landing-navigation {
    gap: 2px;
  }

  .landing-nav-link {
    display: none;
  }

  .landing-admin-link {
    min-height: 38px;
    padding-inline: 11px;
    font-size: 0.88rem;
  }

  .landing-hero {
    padding: 50px 0 72px;
  }

  .landing-hero h1 {
    font-size: clamp(2.55rem, 12vw, 4rem);
  }

  .landing-lead {
    margin-top: 22px;
    font-size: 1rem;
  }

  .landing-hero-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .landing-trust-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }

  .landing-product-window {
    min-height: 430px;
    border-radius: 14px;
  }

  .landing-window-bar {
    grid-template-columns: 1fr auto;
  }

  .landing-window-title {
    display: none;
  }

  .landing-workspace-preview {
    height: 382px;
    grid-template-columns: 94px minmax(210px, 1fr);
  }

  .landing-preview-sidebar {
    padding: 13px 8px;
  }

  .landing-preview-search {
    padding: 0;
    justify-content: center;

    span {
      display: none;
    }
  }

  .landing-preview-brand {
    strong {
      display: none;
    }
  }

  .landing-preview-note {
    justify-content: center;

    span {
      display: none;
    }
  }

  .landing-preview-document {
    padding: 35px 22px;
  }

  .landing-preview-agent {
    display: none;
  }

  .landing-capabilities {
    padding: 76px 0;
  }

  .landing-capability-grid {
    grid-template-columns: 1fr;
    margin-top: 35px;

    article {
      min-height: 0;
      padding: 24px;
    }
  }

  .landing-space-entry {
    margin-bottom: 72px;
    padding: 26px 20px;
  }

  .landing-space-entry-copy {
    align-items: flex-start;
  }

  .landing-space-control {
    height: auto;
    padding: 5px;
    flex-direction: column;

    input {
      min-height: 43px;
    }

    button {
      min-height: 42px;
    }
  }

  .landing-footer {
    padding: 28px 0;
    grid-template-columns: 1fr auto;

    > p {
      display: none;
    }
  }
}

@media (max-width: 390px) {
  .landing-trust-row {
    grid-template-columns: 1fr;
  }

  .landing-space-entry-copy {
    gap: 12px;
  }

  .landing-space-entry-icon {
    width: 42px;
    height: 42px;
    flex-basis: 42px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .landing-page {
    scroll-behavior: auto;
  }

  .landing-primary-action,
  .landing-secondary-action,
  .landing-capability-grid article {
    transition: none;
  }
}
</style>
